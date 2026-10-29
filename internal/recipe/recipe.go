// Package recipe validates UGT recipe records against the published schema.
package recipe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// ValidateDir reports whether dir holds a valid recipe: a recipe.json that
// matches the schema and a compose.yaml that keeps the recipe rules.
func ValidateDir(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "recipe.json"))
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
	compose, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
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
