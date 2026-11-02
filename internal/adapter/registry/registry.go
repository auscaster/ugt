// Package registry fetches UGT's Linux layer from the OCI registry it is
// published in, anonymously, by the distribution API: the image for one
// architecture, whose one file system layer is the layer. Each download is
// checked against the digest its manifest names, and kept, so a slow
// connection never fetches the same layer twice.
package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Media types of the manifests a layer is published with.
const (
	ociIndex       = "application/vnd.oci.image.index.v1+json"
	ociManifest    = "application/vnd.oci.image.manifest.v1+json"
	dockerList     = "application/vnd.docker.distribution.manifest.list.v2+json"
	dockerManifest = "application/vnd.docker.distribution.manifest.v2+json"
)

// Client reads from registries over HTTPS.
type Client struct {
	HTTP *http.Client
}

type descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	Platform  *struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
	} `json:"platform,omitempty"`
}

type manifest struct {
	MediaType string       `json:"mediaType"`
	Manifests []descriptor `json:"manifests"` // an index
	Layers    []descriptor `json:"layers"`    // an image
}

// Layer downloads image's file system for arch (amd64, arm64) into dir, as
// a gzipped tar named by its digest, and returns its path. A download already
// there that matches the digest is used again.
func (c *Client) Layer(ctx context.Context, image, arch, dir string, progress func(string)) (string, error) {
	host, repo, ref, err := parse(image)
	if err != nil {
		return "", err
	}
	s := &session{http: c.HTTP, host: host, repo: repo}
	if s.http == nil {
		s.http = http.DefaultClient
	}
	m, err := s.manifest(ctx, ref)
	if err != nil {
		return "", err
	}
	if len(m.Manifests) > 0 {
		var found string
		for _, d := range m.Manifests {
			if d.Platform != nil && d.Platform.OS == "linux" && d.Platform.Architecture == arch {
				found = d.Digest
			}
		}
		if found == "" {
			return "", fmt.Errorf("%s has no layer for linux/%s", image, arch)
		}
		if m, err = s.manifest(ctx, found); err != nil {
			return "", err
		}
	}
	if len(m.Layers) != 1 {
		return "", fmt.Errorf("%s has %d file system layers, where UGT's layer has one", image, len(m.Layers))
	}
	blob := m.Layers[0]
	sum, ok := strings.CutPrefix(blob.Digest, "sha256:")
	if !ok || len(sum) != 64 {
		return "", fmt.Errorf("%s's layer has digest %q, not a SHA-256", image, blob.Digest)
	}
	dest := filepath.Join(dir, sum+".tar.gz")
	if have, err := fileSum(dest); err == nil && have == sum {
		return dest, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	progress(fmt.Sprintf("downloading UGT's Linux layer (%d MB)", (blob.Size+(1<<20)-1)>>20))
	resp, err := s.get(ctx, "/blobs/"+blob.Digest, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	tmp, err := os.CreateTemp(dir, "layer-*.part")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, blob.Size+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("download %s's layer: %w", image, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); n != blob.Size || got != sum {
		return "", fmt.Errorf("%s's layer arrived as %d bytes with SHA-256 %s, where its manifest says %d bytes and %s: not using it", image, n, got, blob.Size, sum)
	}
	return dest, os.Rename(tmp.Name(), dest)
}

// parse splits host/repository:tag, or @digest.
func parse(image string) (host, repo, ref string, err error) {
	host, rest, ok := strings.Cut(image, "/")
	if !ok || !strings.Contains(host, ".") {
		return "", "", "", fmt.Errorf("%q does not name its registry", image)
	}
	if name, digest, ok := strings.Cut(rest, "@"); ok {
		return host, name, digest, nil
	}
	i := strings.LastIndex(rest, ":")
	if i < 0 {
		return "", "", "", fmt.Errorf("%q names no tag or digest", image)
	}
	return host, rest[:i], rest[i+1:], nil
}

// session is one repository, with the token the registry gave for it.
type session struct {
	http       *http.Client
	host, repo string
	token      string
}

func (s *session) manifest(ctx context.Context, ref string) (manifest, error) {
	resp, err := s.get(ctx, "/manifests/"+ref, strings.Join([]string{ociIndex, ociManifest, dockerList, dockerManifest}, ", "))
	if err != nil {
		return manifest{}, err
	}
	defer resp.Body.Close()
	var m manifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&m); err != nil {
		return manifest{}, fmt.Errorf("read %s/%s's manifest %s: %w", s.host, s.repo, ref, err)
	}
	return m, nil
}

// get fetches a path in the repository, answering the registry's challenge
// for an anonymous pull token once.
func (s *session) get(ctx context.Context, p, accept string) (*http.Response, error) {
	target := "https://" + s.host + "/v2/" + s.repo + p
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, err
		}
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		if s.token != "" {
			req.Header.Set("Authorization", "Bearer "+s.token)
		}
		resp, err := s.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("reach %s: %w", s.host, err)
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			challenge := resp.Header.Get("WWW-Authenticate")
			resp.Body.Close()
			if s.token, err = s.authorize(ctx, challenge); err != nil {
				return nil, err
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("%s answered %s for %s%s", s.host, resp.Status, s.repo, p)
		}
		return resp, nil
	}
}

// authorize asks the realm a Bearer challenge names for an anonymous token.
func (s *session) authorize(ctx context.Context, challenge string) (string, error) {
	scheme, params, _ := strings.Cut(challenge, " ")
	if !strings.EqualFold(scheme, "Bearer") {
		return "", fmt.Errorf("%s asks for %q authorization, where UGT pulls anonymously", s.host, scheme)
	}
	fields := map[string]string{}
	for _, part := range strings.Split(params, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		fields[k] = strings.Trim(v, `"`)
	}
	realm, err := url.Parse(fields["realm"])
	if err != nil || realm.Scheme != "https" {
		return "", fmt.Errorf("%s names no HTTPS token service: %q", s.host, challenge)
	}
	q := realm.Query()
	for _, k := range []string{"service", "scope"} {
		if fields[k] != "" {
			q.Set(k, fields[k])
		}
	}
	realm.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("reach %s's token service: %w", s.host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s's token service answered %s", s.host, resp.Status)
	}
	var t struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&t); err != nil {
		return "", err
	}
	if t.Token == "" {
		t.Token = t.AccessToken
	}
	if t.Token == "" {
		return "", errors.New(s.host + "'s token service gave no token")
	}
	return t.Token, nil
}

func fileSum(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
