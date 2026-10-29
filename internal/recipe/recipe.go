// Package recipe validates UGT recipe records against the published schema.
package recipe

import (
	"bytes"
	"errors"
	"fmt"
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

// Validate reports whether data is a valid recipe record. The returned error
// lists every schema violation, followed by the schema's own description of
// each failed rule that the library reports only by keyword ("'not' failed").
func Validate(data []byte) error {
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
