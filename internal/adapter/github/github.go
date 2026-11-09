// Package github implements the GitHub port: it reads public repositories
// through GitHub's REST API, and file contents from its raw file host, which
// has no rate limit; signs the user in with GitHub's device flow; and opens
// recipe pull requests as them. A token is optional for reads and only
// raises the API's limit.
package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/usegeekthing/ugt/internal/app"
)

// Client reads public repositories, signs the user in, and proposes recipes
// as them.
type Client struct {
	API  string // https://api.github.com; tests point it at a fake
	Raw  string // https://raw.githubusercontent.com
	Web  string // https://github.com, where the device flow runs
	HTTP *http.Client
	// ClientID is UGT's OAuth app on GitHub, whose device flow signs the
	// user in. It is public, as every command-line app's is.
	ClientID string
	// Token is the user's GitHub token, or "" without a login. Reads need
	// none; it only raises their rate limit. Proposing needs it.
	Token func() string
}

var _ app.GitHub = (*Client)(nil)

// New returns a client for github.com. token returns the user's token, or
// "" when they have none; it is asked only when a request is made.
func New(token func() string, clientID string) *Client {
	return &Client{
		API:      "https://api.github.com",
		Raw:      "https://raw.githubusercontent.com",
		Web:      "https://github.com",
		Token:    token,
		ClientID: clientID,
		HTTP:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Scope is what signing in grants: forking a public repository and opening
// a pull request from the fork, as the user. A GitHub App's user token cannot
// write to a fork, whose account the app is not installed on.
const Scope = "public_repo"

func (c *Client) StartLogin(ctx context.Context) (app.DeviceLogin, error) {
	if c.ClientID == "" {
		return app.DeviceLogin{}, errors.New("this ugt was built without UGT's GitHub sign-in: set GITHUB_TOKEN instead")
	}
	var body struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
		Error           string `json:"error_description"`
	}
	if err := c.form(ctx, "/login/device/code", url.Values{"client_id": {c.ClientID}, "scope": {Scope}}, &body); err != nil {
		return app.DeviceLogin{}, err
	}
	if body.DeviceCode == "" {
		return app.DeviceLogin{}, fmt.Errorf("GitHub did not start the sign-in: %s", body.Error)
	}
	return app.DeviceLogin{
		Code: body.UserCode, URL: body.VerificationURI, Device: body.DeviceCode,
		Expires: time.Now().Add(time.Duration(body.ExpiresIn) * time.Second), Interval: time.Duration(body.Interval) * time.Second,
	}, nil
}

// minPoll is the shortest wait between asking whether a login was approved.
var minPoll = time.Second

func (c *Client) FinishLogin(ctx context.Context, l app.DeviceLogin) (string, string, error) {
	interval := max(l.Interval, minPoll)
	for {
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(interval):
		}
		var body struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
			Interval    int    `json:"interval"`
		}
		err := c.form(ctx, "/login/oauth/access_token", url.Values{
			"client_id": {c.ClientID}, "device_code": {l.Device}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"},
		}, &body)
		if err != nil {
			return "", "", err
		}
		switch body.Error {
		case "":
			var user struct {
				Login string `json:"login"`
			}
			if err := c.callAs(ctx, body.AccessToken, http.MethodGet, "/user", nil, &user); err != nil {
				return "", "", err
			}
			return body.AccessToken, user.Login, nil
		case "authorization_pending":
		case "slow_down":
			interval = time.Duration(body.Interval) * time.Second
		case "expired_token":
			return "", "", errors.New("the sign-in code expired before it was approved: sign in again")
		case "access_denied":
			return "", "", errors.New("the sign-in was declined on GitHub")
		default:
			return "", "", fmt.Errorf("GitHub refused the sign-in: %s", body.Error)
		}
	}
}

// form posts a form to github.com's own endpoints and reads the JSON answer.
func (c *Client) form(ctx context.Context, p string, values url.Values, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Web+p, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ugt")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach GitHub to sign in: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("GitHub answered %s to the sign-in: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func (c *Client) token() string {
	if c.Token == nil {
		return ""
	}
	return c.Token()
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

func (c *Client) Licence(ctx context.Context, owner, name, commit string) (string, error) {
	var body struct {
		License struct {
			SPDX string `json:"spdx_id"`
		} `json:"license"`
	}
	p := fmt.Sprintf("/repos/%s/%s/license?ref=%s", owner, name, url.QueryEscape(commit))
	err := c.getJSON(ctx, p, "licence of "+owner+"/"+name+" at "+commit, &body)
	if errors.Is(err, app.ErrNotFound) || (err == nil && body.License.SPDX == "NOASSERTION") {
		return "", nil
	}
	return body.License.SPDX, err
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
	c.authorize(req, accept)
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

// authorize sets a request's headers: the API's media type and version when
// accept is given, and the user's token when there is one, which the raw file
// host also takes for a private repository.
func (c *Client) authorize(req *http.Request, accept string) {
	if accept != "" {
		req.Header.Set("Accept", accept)
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	if t := c.token(); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	req.Header.Set("User-Agent", "ugt")
}

// escapePath escapes each segment of a slash path.
func escapePath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// Propose opens or updates a pull request that sets one folder's files. It
// writes one commit through the Git data API: a blob per file, a tree over
// the default branch's that removes the folder's other files, the commit,
// then the branch, in the repository or in the user's fork of it.
func (c *Client) Propose(ctx context.Context, p app.Proposal) (string, error) {
	if c.token() == "" {
		return "", app.ErrNoLogin
	}
	var repo struct {
		DefaultBranch string `json:"default_branch"`
		Permissions   struct {
			Push bool `json:"push"`
		} `json:"permissions"`
	}
	if err := c.call(ctx, http.MethodGet, "/repos/"+p.Repo, nil, &repo); err != nil {
		return "", err
	}
	var user struct {
		Login string `json:"login"`
	}
	if err := c.call(ctx, http.MethodGet, "/user", nil, &user); err != nil {
		return "", err
	}
	target, head := p.Repo, p.Branch
	headOwner, _, _ := strings.Cut(p.Repo, "/")
	if !repo.Permissions.Push {
		var fork struct {
			FullName string `json:"full_name"`
		}
		if err := c.call(ctx, http.MethodPost, "/repos/"+p.Repo+"/forks", map[string]any{"default_branch_only": true}, &fork); err != nil {
			return "", fmt.Errorf("fork %s: %w", p.Repo, err)
		}
		if err := c.waitFor(ctx, "/repos/"+fork.FullName); err != nil {
			return "", err
		}
		target, head, headOwner = fork.FullName, user.Login+":"+p.Branch, user.Login
	}

	var base struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.call(ctx, http.MethodGet, "/repos/"+p.Repo+"/git/ref/heads/"+repo.DefaultBranch, nil, &base); err != nil {
		return "", err
	}
	var baseCommit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := c.call(ctx, http.MethodGet, "/repos/"+p.Repo+"/git/commits/"+base.Object.SHA, nil, &baseCommit); err != nil {
		return "", err
	}
	existing, err := c.folderFiles(ctx, p.Repo, base.Object.SHA, p.Dir)
	if err != nil {
		return "", err
	}

	var entries []map[string]any
	names := make([]string, 0, len(p.Files))
	for name := range p.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		var blob struct {
			SHA string `json:"sha"`
		}
		body := map[string]any{"content": base64.StdEncoding.EncodeToString(p.Files[name]), "encoding": "base64"}
		if err := c.call(ctx, http.MethodPost, "/repos/"+target+"/git/blobs", body, &blob); err != nil {
			return "", fmt.Errorf("upload %s: %w", name, err)
		}
		entries = append(entries, map[string]any{"path": p.Dir + "/" + name, "mode": "100644", "type": "blob", "sha": blob.SHA})
	}
	for _, rel := range existing {
		if p.Files[rel] == nil {
			entries = append(entries, map[string]any{"path": p.Dir + "/" + rel, "mode": "100644", "type": "blob", "sha": nil})
		}
	}
	var tree, commit struct {
		SHA string `json:"sha"`
	}
	if err := c.call(ctx, http.MethodPost, "/repos/"+target+"/git/trees", map[string]any{"base_tree": baseCommit.Tree.SHA, "tree": entries}, &tree); err != nil {
		return "", err
	}
	if err := c.call(ctx, http.MethodPost, "/repos/"+target+"/git/commits",
		map[string]any{"message": p.Message, "tree": tree.SHA, "parents": []string{base.Object.SHA}}, &commit); err != nil {
		return "", err
	}
	ref := map[string]any{"ref": "refs/heads/" + p.Branch, "sha": commit.SHA}
	if err := c.call(ctx, http.MethodPost, "/repos/"+target+"/git/refs", ref, nil); err != nil {
		// the branch is there from an earlier proposal: move it
		if err := c.call(ctx, http.MethodPatch, "/repos/"+target+"/git/refs/heads/"+p.Branch, map[string]any{"sha": commit.SHA, "force": true}, nil); err != nil {
			return "", err
		}
	}

	var pr struct {
		HTMLURL string `json:"html_url"`
	}
	err = c.call(ctx, http.MethodPost, "/repos/"+p.Repo+"/pulls",
		map[string]any{"title": p.Title, "head": head, "base": repo.DefaultBranch, "body": p.Body}, &pr)
	if err == nil {
		return pr.HTMLURL, nil
	}
	// the pull request is there from an earlier proposal, now updated
	var open []struct {
		HTMLURL string `json:"html_url"`
	}
	if lerr := c.call(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls?state=open&head=%s:%s", p.Repo, headOwner, url.QueryEscape(p.Branch)), nil, &open); lerr == nil && len(open) > 0 {
		return open[0].HTMLURL, nil
	}
	return "", err
}

// folderFiles lists the files in one folder of a repository at a commit,
// relative to it: one listing of its parent and one of its own tree, so the
// cost never depends on the size of the repository. A folder not there yet
// has none.
func (c *Client) folderFiles(ctx context.Context, repo, commit, dir string) ([]string, error) {
	parent, name := path.Split(dir)
	var siblings []struct {
		Name string `json:"name"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
	}
	err := c.call(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repo, escapePath(parent), commit), nil, &siblings)
	if errors.Is(err, app.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, s := range siblings {
		if s.Name != name || s.Type != "dir" {
			continue
		}
		var tree struct {
			Tree []struct {
				Path string `json:"path"`
				Type string `json:"type"`
			} `json:"tree"`
		}
		if err := c.call(ctx, http.MethodGet, "/repos/"+repo+"/git/trees/"+s.SHA+"?recursive=1", nil, &tree); err != nil {
			return nil, err
		}
		var files []string
		for _, e := range tree.Tree {
			if e.Type == "blob" {
				files = append(files, e.Path)
			}
		}
		return files, nil
	}
	return nil, nil
}

// waitFor waits for a new fork to answer, as GitHub creates it in the background.
func (c *Client) waitFor(ctx context.Context, path string) error {
	for i := 0; i < 30; i++ {
		if err := c.call(ctx, http.MethodGet, path, nil, nil); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("GitHub did not finish creating %s", path)
}

// call makes an API request as the user, with a JSON body and result.
func (c *Client) call(ctx context.Context, method, path string, body, result any) error {
	return c.callAs(ctx, c.token(), method, path, body, result)
}

// callAs makes an API request with a token.
func (c *Client) callAs(ctx context.Context, token, method, path string, body, result any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.API+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "ugt")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("GitHub %s %s: %w (or your login cannot see it)", method, path, app.ErrNotFound)
		}
		return fmt.Errorf("GitHub %s %s answered %s: %s", method, path, resp.Status, strings.TrimSpace(string(msg)))
	}
	if result == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(result)
}
