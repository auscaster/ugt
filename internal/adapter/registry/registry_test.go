package registry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	serveBlob  func(http.ResponseWriter, *http.Request, []byte)
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
			if r.serveBlob != nil {
				r.serveBlob(w, req, r.blobs[ref])
				return
			}
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

// A real HTTP response can close after it delivered part of a large setup
// image. Continue the anonymous pull and publish only the manifest's bytes.
func TestInterruptedLayer(t *testing.T) {
	for _, kind := range []string{"range", "full", "wrong offset", "corruption", "interrupted again"} {
		t.Run(kind, func(t *testing.T) {
			r, srv := newRegistry(t)
			r.serveBlob = func(w http.ResponseWriter, req *http.Request, data []byte) {
				if r.blobGets == 1 {
					w.Header().Set("Content-Length", fmt.Sprint(len(data)))
					_, _ = w.Write(data[:4])
					return
				}
				if kind == "interrupted again" {
					var start int
					if _, err := fmt.Sscanf(req.Header.Get("Range"), "bytes=%d-", &start); err != nil {
						t.Error(err)
						return
					}
					w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
					w.Header().Set("Content-Length", fmt.Sprint(len(data)-start))
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write(data[start : start+1])
					return
				}
				if v := req.Header.Get("Range"); v != "bytes=4-" {
					t.Errorf("next request range %q", v)
				}
				switch kind {
				case "full":
					_, _ = w.Write(data)
					return
				case "wrong offset":
					w.Header().Set("Content-Range", fmt.Sprintf("bytes 1-%d/%d", len(data)-1, len(data)))
				default:
					w.Header().Set("Content-Range", fmt.Sprintf("bytes 4-%d/%d", len(data)-1, len(data)))
				}
				w.WriteHeader(http.StatusPartialContent)
				if kind == "corruption" {
					_, _ = w.Write(bytes.Repeat([]byte("!"), len(data)-4))
					return
				}
				_, _ = w.Write(data[4:])
			}
			dir := t.TempDir()
			path, err := (&Client{HTTP: srv.Client()}).Layer(context.Background(), strings.TrimPrefix(srv.URL, "https://")+"/acme/layer:abc123", "arm64", dir, func(string) {})
			if kind == "range" || kind == "full" {
				if err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(path)
				if err != nil || string(got) != "the arm64 layer" {
					t.Fatalf("installed %q, %v", got, err)
				}
			} else {
				if err == nil {
					t.Fatal("accepted an incomplete or corrupt layer")
				}
				if files, e := os.ReadDir(dir); e != nil || len(files) != 0 {
					t.Fatalf("kept files %v, %v", files, e)
				}
			}
			wantGets := 2
			if kind == "interrupted again" {
				wantGets = 3
			}
			if r.blobGets != wantGets {
				t.Errorf("%d blob requests, want %d", r.blobGets, wantGets)
			}
		})
	}
}
