package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/auscaster/ugt/internal/domain/compose"
	"github.com/auscaster/ugt/internal/domain/recipe"
)

// NewOptions says which recipe to start, and from what.
type NewOptions struct {
	Project string // owner/repo, or a github.com link into it
	Task    string // the task slug: what the recipe produces, as subtitles or markdown
	What    string // the task in plain words; made from the slug when empty
	Kind    string // job or service; inferred when empty
	// From is the Compose file to adopt: a path in the project, a GitHub link
	// to a file in any repository, or a file on this machine. Empty looks for
	// the project's own.
	From  string
	Blank bool   // start blank even when the project ships a Compose file
	Dir   string // where to write the recipe; the local catalogue when empty
}

// NewResult is a recipe started by New, and what its author does next.
type NewResult struct {
	Recipe   string   `json:"recipe"`
	Dir      string   `json:"dir"`
	Commit   string   `json:"commit"`
	Kind     string   `json:"kind"`
	Adopted  string   `json:"adopted,omitempty"` // the Compose file it started from
	Files    []string `json:"files"`
	Changes  []string `json:"changes,omitempty"`  // what adoption rewrote
	Problems []string `json:"problems,omitempty"` // what the author must resolve first
	Builds   []string `json:"builds,omitempty"`   // adopted services that build from source
	Next     []string `json:"next"`
}

// composeNames are the file names Compose itself looks for, in its order.
var composeNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// fetchLimit bounds how many files adoption copies for one mounted folder.
const fetchLimit = 200

// New writes a new recipe folder that already keeps the recipe rules: from the
// project's own Compose file or another one when there is one to adopt,
// otherwise blank, with a Dockerfile building the pinned commit. Its services
// are placeholders that say what to write and fail until written.
func (a *App) New(ctx context.Context, opts NewOptions) (NewResult, error) {
	owner, name, err := recipe.ParseProject(opts.Project)
	if err != nil {
		return NewResult{}, err
	}
	id, err := recipe.ParseID(strings.ToLower(owner+"/"+name) + "/" + opts.Task)
	if err != nil {
		return NewResult{}, err
	}
	if opts.Kind != "" && opts.Kind != recipe.KindJob && opts.Kind != recipe.KindService {
		return NewResult{}, fmt.Errorf("kind %q is neither job nor service", opts.Kind)
	}
	dir := opts.Dir
	if dir == "" {
		dir = a.Layout.LocalRecipe(id)
	}
	if exists, err := a.Disk.Exists(filepath.Join(dir, recipe.RecordFile)); err != nil {
		return NewResult{}, err
	} else if exists {
		return NewResult{}, fmt.Errorf("a recipe already exists at %s: edit it there, or choose another folder", dir)
	}

	repo, err := a.GitHub.Repo(ctx, owner, name)
	if err != nil {
		return NewResult{}, err
	}
	commit, err := a.GitHub.Commit(ctx, repo.Owner, repo.Name, repo.DefaultBranch)
	if err != nil {
		return NewResult{}, err
	}
	draft := recipe.Draft{
		ID:       id,
		Repo:     "https://github.com/" + repo.Owner + "/" + repo.Name,
		Commit:   commit,
		What:     opts.What,
		Kind:     opts.Kind,
		Licence:  repo.Licence,
		Language: repo.Language,
	}
	if draft.What == "" {
		draft.What = describe(opts.Task, repo)
	}
	if draft.Kind == "" {
		draft.Kind = recipe.KindJob
	}
	res := NewResult{Recipe: id.String(), Dir: dir, Commit: commit}

	upstream := map[string][]byte{}
	o, data, err := a.composeSource(ctx, opts, repo, commit)
	if err != nil {
		return NewResult{}, err
	}
	if data != nil {
		pins, err := a.pins(ctx, data)
		if err != nil {
			return NewResult{}, err
		}
		adoption, err := compose.Adopt(data, o.Source, pins, opts.Kind)
		if err != nil {
			return NewResult{}, fmt.Errorf("adopt %s: %w", o.Name, err)
		}
		for _, f := range adoption.Fetch {
			got, err := a.fetch(ctx, o, f)
			if err != nil {
				adoption.Problems = append(adoption.Problems, fmt.Sprintf(
					"the original reads %s, which could not be copied (%v): add it to the recipe at %s, or remove what reads it", f.From, err, f.To))
				continue
			}
			for p, b := range got {
				upstream[p] = b
			}
		}
		draft.Compose, draft.Kind, draft.Settings, draft.Access = adoption.Compose, adoption.Kind, adoption.Settings, adoption.Access
		draft.Provides = adoption.Provides
		draft.Source, draft.AdaptedFrom, draft.Copied = o.Name, o.url, len(upstream) > 0
		res.Adopted, res.Changes, res.Problems = o.Name, adoption.Changes, adoption.Problems
		res.Builds = adoption.Builds
	}
	res.Kind = draft.Kind
	if data == nil && draft.Kind == recipe.KindService {
		draft.Provides.Pages = []recipe.Address{{Service: recipe.DraftService, Port: recipe.DraftPort, What: draft.What}}
	}
	if data == nil {
		var bases []string
		for _, b := range draft.Bases() {
			bases = append(bases, b.Ref)
		}
		if draft.BasePins, err = a.digests(ctx, bases, "drafting a recipe pins its base images by digest"); err != nil {
			return NewResult{}, err
		}
	}

	files, err := draft.Files()
	if err != nil {
		return NewResult{}, err
	}
	for p, b := range upstream {
		files[p] = b
	}
	for _, p := range sortedKeys(files) {
		if err := a.Disk.WriteFile(filepath.Join(dir, filepath.FromSlash(p)), files[p]); err != nil {
			return NewResult{}, err
		}
		res.Files = append(res.Files, p)
	}
	// What New writes keeps the rules; only what adoption could not fix may fail.
	if err := recipe.Validate(a.Disk.FS(dir)); err != nil && len(res.Problems) == 0 {
		return res, fmt.Errorf("the new recipe at %s does not validate, which is a bug in ugt: %w", dir, err)
	}
	res.Next = nextSteps(res, files)
	return res, nil
}

// origin is where an adopted Compose file was read from.
type origin struct {
	compose.Source
	url         string // its permanent URL, for adapted_from
	owner, name string // its repository, for a file on GitHub
	local       string // its folder on this machine, for a local file
}

var githubFile = regexp.MustCompile(`^https://(?:github\.com/([^/]+)/([^/]+)/blob|raw\.githubusercontent\.com/([^/]+)/([^/]+))/([^/]+)/(.+)$`)

// composeSource finds the Compose file to adopt and reads it. It returns no
// data for a blank recipe.
func (a *App) composeSource(ctx context.Context, opts NewOptions, repo Repo, commit string) (origin, []byte, error) {
	if opts.Blank {
		return origin{}, nil, nil
	}
	from := opts.From
	if from == "" {
		entries, err := a.GitHub.List(ctx, repo.Owner, repo.Name, commit, "")
		if err != nil {
			return origin{}, nil, err
		}
		for _, candidate := range composeNames {
			for _, e := range entries {
				if !e.Dir && e.Path == candidate && from == "" {
					from = candidate
				}
			}
		}
		if from == "" {
			return origin{}, nil, nil
		}
	}
	if m := githubFile.FindStringSubmatch(from); m != nil {
		owner, name, ref, file := m[1]+m[3], m[2]+m[4], m[5], m[6]
		sha, err := a.GitHub.Commit(ctx, owner, name, ref)
		if err != nil {
			return origin{}, nil, err
		}
		return a.readGitHub(ctx, owner, name, sha, file)
	}
	if strings.Contains(from, "://") {
		return origin{}, nil, fmt.Errorf("--from %s: give a github.com link to the file, a path in the project, or a file on this machine", from)
	}
	if exists, err := a.Disk.Exists(from); err != nil {
		return origin{}, nil, err
	} else if exists {
		dir, base := filepath.Split(from)
		data, err := fs.ReadFile(a.Disk.FS(filepath.Clean(dir)), base)
		if err != nil {
			return origin{}, nil, err
		}
		o := origin{local: filepath.Clean(dir)}
		o.Name = base + " from this machine"
		return o, data, nil
	}
	return a.readGitHub(ctx, repo.Owner, repo.Name, commit, strings.TrimPrefix(path.Clean(from), "/"))
}

func (a *App) readGitHub(ctx context.Context, owner, name, commit, file string) (origin, []byte, error) {
	data, err := a.GitHub.File(ctx, owner, name, commit, file)
	if err != nil {
		return origin{}, nil, err
	}
	o := origin{
		url:   fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s", owner, name, commit, file),
		owner: owner,
		name:  name,
	}
	o.Source = compose.Source{
		Repo:   owner + "/" + name,
		Commit: commit,
		Dir:    strings.TrimPrefix(path.Dir(file), "."),
		Name:   file + " in " + owner + "/" + name,
	}
	return o, data, nil
}

// pins resolves the digest of every image the file needs pinned. An image that
// cannot be resolved is left for Adopt to report.
func (a *App) pins(ctx context.Context, data []byte) (map[string]string, error) {
	images, err := compose.Images(data)
	if err != nil {
		return nil, err
	}
	return a.digests(ctx, images, "adopting a Compose file pins its images by digest")
}

// digests reads each image's digest from its registry; one that cannot be
// read is left out, for the caller to report.
func (a *App) digests(ctx context.Context, refs []string, why string) (map[string]string, error) {
	status, err := a.Runtime.Status(ctx)
	if err != nil {
		return nil, err
	}
	if !status.Ready {
		return nil, fmt.Errorf("%s, which needs the engine: %s %s", why, status.Problem, status.Fix)
	}
	pins := map[string]string{}
	for _, ref := range refs {
		if digest, err := a.Engine.ImageDigest(ctx, ref); err == nil {
			pins[ref] = digest
		}
	}
	return pins, nil
}

// fetch copies a file or folder the original reads, by its path in the
// recipe folder.
func (a *App) fetch(ctx context.Context, o origin, f compose.Fetch) (map[string][]byte, error) {
	out := map[string][]byte{}
	if o.local != "" {
		fsys := a.Disk.FS(o.local)
		err := fs.WalkDir(fsys, f.From, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if len(out) >= fetchLimit {
				return fmt.Errorf("more than %d files", fetchLimit)
			}
			data, err := fs.ReadFile(fsys, p)
			out[path.Join(f.To, strings.TrimPrefix(p, f.From))] = data
			return err
		})
		return out, err
	}
	data, err := a.GitHub.File(ctx, o.owner, o.name, o.Commit, f.From)
	if err == nil {
		out[f.To] = data
		return out, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	// not a file: perhaps a folder
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := a.GitHub.List(ctx, o.owner, o.name, o.Commit, dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.Dir {
				if err := walk(e.Path); err != nil {
					return err
				}
				continue
			}
			if len(out) >= fetchLimit {
				return fmt.Errorf("more than %d files", fetchLimit)
			}
			data, err := a.GitHub.File(ctx, o.owner, o.name, o.Commit, e.Path)
			if err != nil {
				return err
			}
			out[path.Join(f.To, strings.TrimPrefix(e.Path, f.From))] = data
		}
		return nil
	}
	if werr := walk(f.From); werr != nil {
		if errors.Is(werr, ErrNotFound) {
			return nil, err // the file's own not-found says more
		}
		return nil, werr
	}
	return out, nil
}

var placeholderService = regexp.MustCompile(regexp.QuoteMeta(recipe.ScaffoldMark) + ` write the ([a-z-]+) service`)

// nextSteps says what the author does next, in order.
func nextSteps(res NewResult, files map[string][]byte) []string {
	var steps []string
	if len(res.Problems) > 0 {
		steps = append(steps, "Resolve each problem above in compose.yaml: the recipe fails validation until they are gone.")
	}
	if len(res.Builds) > 0 {
		verb := "build"
		if len(res.Builds) == 1 {
			verb = "builds"
		}
		steps = append(steps, fmt.Sprintf("%s %s from source at the pinned commit, which can be slow. If upstream publishes images, use them instead, pinned by digest.",
			joinWords(res.Builds), verb))
	}
	if _, blank := files["Dockerfile"]; blank {
		steps = append(steps, "Finish the Dockerfile: install the project the way its README says.")
	}
	var placeholders []string
	for _, m := range placeholderService.FindAllStringSubmatch(string(files[recipe.ComposeFile]), -1) {
		placeholders = append(placeholders, m[1])
	}
	if len(placeholders) > 0 {
		steps = append(steps, fmt.Sprintf("Write the %s services in compose.yaml. Each one now says what it must do and fails until it is written.", joinWords(placeholders)))
	}
	record := "Fill in inputs, outputs and known_issues in recipe.json"
	if res.Kind == recipe.KindService {
		record = "Fill in inputs, provides and known_issues in recipe.json: each port the user opens as a page, each one software calls as an endpoint"
	}
	if strings.Contains(string(files[recipe.RecordFile]), `"settings"`) {
		record += ", and say what each setting is and how to get it"
	}
	steps = append(steps, record+". Add commands and MCP servers to provides when the project has them.")
	steps = append(steps, fmt.Sprintf("Run ugt recipe verify %q until it passes.", res.Dir))
	return steps
}

// describe makes a task description from its slug: "subtitles" for
// openai/whisper is "Subtitles from openai/whisper".
func describe(task string, repo Repo) string {
	words := strings.ReplaceAll(task, "-", " ")
	return strings.ToUpper(words[:1]) + words[1:] + " from " + repo.Owner + "/" + repo.Name
}

func joinWords(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
