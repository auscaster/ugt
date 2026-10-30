package recipe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
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
// recipe.json that matches the schema and a compose.yaml that keeps the
// recipe rules.
func Validate(fsys fs.FS) error {
	data, err := fs.ReadFile(fsys, RecordFile)
	if err != nil {
		return err
	}
	if err := ValidateRecord(data); err != nil {
		return err
	}
	var record struct {
		Kind     string `json:"kind"`
		Settings []struct {
			Name string `json:"name"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return err
	}
	compose, err := fs.ReadFile(fsys, ComposeFile)
	if err != nil {
		return fmt.Errorf("a recipe runs from its compose.yaml: %w", err)
	}
	names := make([]string, len(record.Settings))
	for i, s := range record.Settings {
		names[i] = s.Name
	}
	if problems := lintCompose(compose, record.Kind, names); len(problems) > 0 {
		return fmt.Errorf("compose.yaml breaks the recipe rules:\n- %s", strings.Join(problems, "\n- "))
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
	err = s.Validate(inst)
	var verr *jsonschema.ValidationError
	if errors.As(err, &verr) {
		if rules := describe(verr); len(rules) > 0 {
			return fmt.Errorf("%w\n%s", err, strings.Join(rules, "\n"))
		}
	}
	return err
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
// (owner/repo/task): it must be valid, its ID must match its folder, and it
// must say where it was verified. It returns one message per problem. A
// recipe is admitted on its own, so admission costs the same however large
// the catalogue grows.
func Admit(fsys fs.FS, dir string) []string {
	f, err := Load(fsys)
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", dir, err)}
	}
	var problems []string
	if f.Recipe.ID != dir {
		problems = append(problems, fmt.Sprintf("%s: id %q does not match its folder", dir, f.Recipe.ID))
	}
	if f.Recipe.Verified == nil || len(f.Recipe.PlatformsTested) == 0 {
		problems = append(problems, fmt.Sprintf("%s: a catalogue recipe records where it was verified: verified and platforms_tested", dir))
	}
	return problems
}
