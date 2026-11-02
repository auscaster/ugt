package registry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// fakeRegistry publishes one layer per architecture, behind an anonymous
// token as GitHub's registry does.
type fakeRegistry struct {
	blobs      map[string][]byte // by digest
	manifests  map[string][]byte // by tag or digest
	blobGets   int
	tokenAsked bool
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newRegistry(t *testing.T, layers ...[]byte) (*fakeRegistry, *httptest.Server) {
	t.Helper()
	r := &fakeRegistry{blobs: map[string][]byte{}, manifests: map[string][]byte{}}
	index := manifest{MediaType: ociIndex}
	for i, arch := range []string{"amd64", "arm64"} {
		blob := []byte("the " + arch + " layer")
		r.blobs[digestOf(blob)] = blob
		image := manifest{MediaType: ociManifest, Layers: []descriptor{{Digest: digestOf(blob), Size: int64(len(blob))}}}
		if i < len(layers) {
			image.Layers = nil
			for _, l := range layers {
				image.Layers = append(image.Layers, descriptor{Digest: digestOf(l), Size: int64(len(l))})
			}
		}
		data, _ := json.Marshal(image)
		r.manifests[digestOf(data)] = data
		d := descriptor{MediaType: ociManifest, Digest: digestOf(data)}
		d.Platform = &struct {
			Architecture string `json:"architecture"`
			OS           string `json:"os"`
		}{arch, "linux"}
		index.Manifests = append(index.Manifests, d)
	}
	r.manifests["abc123"], _ = json.Marshal(index)

	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/token" {
			r.tokenAsked = req.URL.Query().Get("scope") == "repository:acme/layer:pull"
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "anonymous"})
			return
		}
		if req.Header.Get("Authorization") != "Bearer anonymous" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+srv.URL+`/token",service="registry",scope="repository:acme/layer:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		ref, ok := strings.CutPrefix(req.URL.Path, "/v2/acme/layer/manifests/")
		if ok && r.manifests[ref] != nil {
			_, _ = w.Write(r.manifests[ref])
			return
		}
		if ref, ok := strings.CutPrefix(req.URL.Path, "/v2/acme/layer/blobs/"); ok && r.blobs[ref] != nil {
			r.blobGets++
			_, _ = w.Write(r.blobs[ref])
			return
		}
		http.NotFound(w, req)
	}))
	t.Cleanup(srv.Close)
	return r, srv
}

// The layer for this architecture is fetched with an anonymous token,
// checked, kept, and used again with no second download.
func TestLayerIsFetchedOnceAndChecked(t *testing.T) {
	r, srv := newRegistry(t)
	c := &Client{HTTP: srv.Client()}
	dir := t.TempDir()
	image := strings.TrimPrefix(srv.URL, "https://") + "/acme/layer:abc123"
	var told []string
	got, err := c.Layer(context.Background(), image, "arm64", dir, func(s string) { told = append(told, s) })
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(got); string(data) != "the arm64 layer" || !r.tokenAsked || len(told) != 1 {
		t.Fatalf("fetched %q, token asked %v, told %v", data, r.tokenAsked, told)
	}
	if again, err := c.Layer(context.Background(), image, "arm64", dir, func(string) {}); err != nil || again != got || r.blobGets != 1 {
		t.Fatalf("again %s, %v, %d downloads", again, err, r.blobGets)
	}
}

// A layer whose bytes do not match its digest is refused and not kept.
func TestLayerThatDoesNotMatchIsRefused(t *testing.T) {
	r, srv := newRegistry(t)
	for d := range r.blobs {
		r.blobs[d] = append(r.blobs[d], '!')
	}
	dir := t.TempDir()
	_, err := (&Client{HTTP: srv.Client()}).Layer(context.Background(), strings.TrimPrefix(srv.URL, "https://")+"/acme/layer:abc123", "amd64", dir, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "not using it") {
		t.Fatalf("got %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("kept %v", entries)
	}
}

func TestImageWithSeveralLayersIsRefused(t *testing.T) {
	_, srv := newRegistry(t, []byte("base"), []byte("more"))
	_, err := (&Client{HTTP: srv.Client()}).Layer(context.Background(), strings.TrimPrefix(srv.URL, "https://")+"/acme/layer:abc123", "amd64", t.TempDir(), func(string) {})
	if err == nil || !strings.Contains(err.Error(), "2 file system layers") {
		t.Fatalf("got %v", err)
	}
	if !bytes.Contains([]byte(err.Error()), []byte("acme/layer")) {
		t.Errorf("does not name the image: %v", err)
	}
}
