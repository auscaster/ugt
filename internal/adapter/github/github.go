// Package github implements the GitHub port: it reads public repositories
// through GitHub's REST API, and file contents from its raw file host, which
// has no rate limit. A token is optional and only raises the API's limit.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/auscaster/ugt/internal/app"
)

// Client reads public repositories.
type Client struct {
	API   string // https://api.github.com; tests point it at a fake
	Raw   string // https://raw.githubusercontent.com
	Token string
	HTTP  *http.Client
}

var _ app.GitHub = (*Client)(nil)

// New returns a client for github.com. token may be empty.
func New(token string) *Client {
	return &Client{
		API:   "https://api.github.com",
		Raw:   "https://raw.githubusercontent.com",
		Token: token,
		HTTP:  &http.Client{Timeout: 30 * time.Second},
	}
}

// maxFile bounds what File reads: recipes copy configuration, not data.
const maxFile = 10 << 20

func (c *Client) Repo(ctx context.Context, owner, name string) (app.Repo, error) {
	var body struct {
		Name          string                 `json:"name"`
		Owner         struct{ Login string } `json:"owner"`
		DefaultBranch string                 `json:"default_branch"`
		Language      string                 `json:"language"`
		Description   string                 `json:"description"`
		License       *struct {
			SPDX string `json:"spdx_id"`
		} `json:"license"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("/repos/%s/%s", owner, name), "github.com/"+owner+"/"+name, &body); err != nil {
		return app.Repo{}, err
	}
	r := app.Repo{Owner: body.Owner.Login, Name: body.Name, DefaultBranch: body.DefaultBranch,
		Language: body.Language, Description: body.Description}
	if body.License != nil && body.License.SPDX != "NOASSERTION" {
		r.Licence = body.License.SPDX
	}
	return r, nil
}

func (c *Client) Commit(ctx context.Context, owner, name, ref string) (string, error) {
	what := fmt.Sprintf("%s in github.com/%s/%s", ref, owner, name)
	resp, err := c.get(ctx, c.API+fmt.Sprintf("/repos/%s/%s/commits/%s", owner, name, url.PathEscape(ref)),
		"application/vnd.github.sha", what)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	sha, err := io.ReadAll(io.LimitReader(resp.Body, 100))
	if err != nil {
		return "", err
	}
	if s := strings.TrimSpace(string(sha)); len(s) == 40 {
		return s, nil
	}
	return "", fmt.Errorf("GitHub answered %q for %s, not a commit SHA", sha, what)
}

func (c *Client) List(ctx context.Context, owner, name, commit, dir string) ([]app.Entry, error) {
	var body []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	}
	p := fmt.Sprintf("/repos/%s/%s/contents/%s?ref=%s", owner, name, escapePath(dir), commit)
	if err := c.getJSON(ctx, p, fmt.Sprintf("%s/ in github.com/%s/%s", dir, owner, name), &body); err != nil {
		return nil, err
	}
	out := make([]app.Entry, len(body))
	for i, e := range body {
		out[i] = app.Entry{Path: e.Path, Dir: e.Type == "dir"}
	}
	return out, nil
}

func (c *Client) File(ctx context.Context, owner, name, commit, path string) ([]byte, error) {
	resp, err := c.get(ctx, fmt.Sprintf("%s/%s/%s/%s/%s", c.Raw, owner, name, commit, escapePath(path)), "",
		fmt.Sprintf("%s in github.com/%s/%s at %s", path, owner, name, commit))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFile {
		return nil, fmt.Errorf("%s is over %d MB", path, maxFile>>20)
	}
	return data, nil
}

func (c *Client) getJSON(ctx context.Context, path, what string, v any) error {
	resp, err := c.get(ctx, c.API+path, "application/vnd.github+json", what)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

// get fetches a URL and turns GitHub's failures into plain errors.
func (c *Client) get(ctx context.Context, u, accept, what string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
	}
	req.Header.Set("User-Agent", "ugt")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach GitHub for %s: %w", what, err)
	}
	if resp.StatusCode == http.StatusOK {
		return resp, nil
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnprocessableEntity:
		return nil, fmt.Errorf("%s: %w (or it is private)", what, app.ErrNotFound)
	case (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) &&
		resp.Header.Get("X-RateLimit-Remaining") == "0":
		reset := "an hour"
		if t, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			reset = time.Unix(t, 0).Format("15:04")
		}
		return nil, fmt.Errorf("GitHub's rate limit for requests without a token is used up until %s; set GITHUB_TOKEN to raise it", reset)
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	return nil, fmt.Errorf("GitHub answered %s for %s: %s", resp.Status, what, strings.TrimSpace(string(msg)))
}

// escapePath escapes each segment of a slash path.
func escapePath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}
