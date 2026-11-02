package recipe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"github.com/auscaster/ugt/spec"
)

var (
	compileOnce sync.Once
	schema      *jsonschema.Schema
	schemaDoc   any
	compileErr  error
)

func compiled() (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(spec.RecipeV0))
		if err != nil {
			compileErr = fmt.Errorf("parse recipe schema: %w", err)
			return
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource(spec.RecipeV0URL, doc); err != nil {
			compileErr = fmt.Errorf("load recipe schema: %w", err)
			return
		}
		schemaDoc = doc
		schema, compileErr = c.Compile(spec.RecipeV0URL)
	})
	return schema, compileErr
}

// Find returns every recipe folder (a folder holding recipe.json) in fsys, as
// sorted slash paths.
func Find(fsys fs.FS) ([]string, error) {
	var dirs []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == RecordFile {
			dirs = append(dirs, path.Dir(p))
		}
		return nil
	})
	sort.Strings(dirs)
	return dirs, err
}

// Validate reports whether fsys holds a valid recipe at its root: a
// recipe.json that matches the schema, with every access it declares saying
// why and everything it provides saying what, a compose.yaml that keeps the
// recipe's structure and has what it provides, and Dockerfiles that name
// every base by digest. Whether the
// access it declares is what Compose grants depends on the values a run is
// given, so that is checked on the resolved model before anything runs.
func Validate(fsys fs.FS) error {
	data, err := fs.ReadFile(fsys, RecordFile)
	if err != nil {
		return err
	}
	if err := ValidateRecord(data); err != nil {
		return err
	}
	var record Recipe
	if err := json.Unmarshal(data, &record); err != nil {
		return err
	}
	for _, a := range record.Access {
		if strings.HasPrefix(a.Why, ScaffoldMark) {
			return fmt.Errorf("recipe.json declares %s access without saying why: replace %q with why the recipe needs it", a.Kind, a.Why)
		}
	}
	for _, what := range record.Provides.Whats() {
		if strings.HasPrefix(what, ScaffoldMark) {
			return fmt.Errorf("recipe.json provides something without saying what it is: replace %q with what the user gets", what)
		}
	}
	compose, err := fs.ReadFile(fsys, ComposeFile)
	if err != nil {
		return fmt.Errorf("a recipe runs from its compose.yaml: %w", err)
	}
	if problems := lintCompose(compose, record); len(problems) > 0 {
		return fmt.Errorf("compose.yaml breaks the recipe rules:\n- %s", strings.Join(problems, "\n- "))
	}
	if problems := lintBases(fsys, compose); len(problems) > 0 {
		return fmt.Errorf("the recipe's build is not pinned:\n- %s", strings.Join(problems, "\n- "))
	}
	return nil
}

// ValidateRecord reports whether data is a valid recipe.json. The returned
// error lists every schema violation, followed by the schema's own description
// of each failed rule that the library reports only by keyword.
func ValidateRecord(data []byte) error {
	s, err := compiled()
	if err != nil {
		return err
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("not JSON: %w", err)
	}
	if err := readable(inst); err != nil {
		return err
	}
	err = s.Validate(inst)
	var verr *jsonschema.ValidationError
	if errors.As(err, &verr) {
		if rules := describe(verr); len(rules) > 0 {
			return fmt.Errorf("%w\n%s", err, strings.Join(rules, "\n"))
		}
	}
	return err
}

var draft = regexp.MustCompile(`^ugt-draft-(\d+)$`)

// readable refuses a recipe in a format this ugt does not read, saying which:
// a newer one means updating UGT, not a broken recipe.
func readable(inst any) error {
	doc, _ := inst.(map[string]any)
	got, _ := doc["recipe_format"].(string)
	if got == Format {
		return nil
	}
	if m := draft.FindStringSubmatch(got); m != nil {
		n, _ := strconv.Atoi(m[1])
		current, _ := strconv.Atoi(draft.FindStringSubmatch(Format)[1])
		if n > current {
			return fmt.Errorf("this recipe is in format %s, newer than this UGT reads (%s): run ugt update, then try again", got, Format)
		}
	}
	return fmt.Errorf("recipe_format is %q: this UGT reads %s", got, Format)
}

func describe(e *jsonschema.ValidationError) []string {
	var rules []string
	if _, ok := e.ErrorKind.(*kind.Not); ok {
		if d := description(e.SchemaURL); d != "" {
			rules = append(rules, fmt.Sprintf("- at '/%s': %s", strings.Join(e.InstanceLocation, "/"), d))
		}
	}
	for _, c := range e.Causes {
		rules = append(rules, describe(c)...)
	}
	return rules
}

// description returns the "description" of the schema node a URL points at.
func description(url string) string {
	_, pointer, _ := strings.Cut(url, "#")
	node := schemaDoc
	for _, token := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		token = strings.NewReplacer("~1", "/", "~0", "~").Replace(token)
		switch n := node.(type) {
		case map[string]any:
			node = n[token]
		case []any:
			i, err := strconv.Atoi(token)
			if err != nil || i < 0 || i >= len(n) {
				return ""
			}
			node = n[i]
		default:
			return ""
		}
	}
	d, _ := node.(map[string]any)["description"].(string)
	return d
}

// Admit checks one recipe for the catalogue, as the folder dir of a catalogue
// (owner/repo/task): it must be valid and its ID must match its folder. It
// returns one message per problem. A recipe is admitted on its own, so
// admission costs the same however large the catalogue grows.
func Admit(fsys fs.FS, dir string) []string {
	f, err := Load(fsys)
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", dir, err)}
	}
	var problems []string
	if f.Recipe.ID != dir {
		problems = append(problems, fmt.Sprintf("%s: id %q does not match its folder", dir, f.Recipe.ID))
	}
	return problems
}
