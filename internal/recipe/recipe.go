// Package recipe validates UGT recipe records against the published schema.
package recipe

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/auscaster/ugt/spec"
)

var (
	compileOnce sync.Once
	schema      *jsonschema.Schema
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
		schema, compileErr = c.Compile(spec.RecipeV0URL)
	})
	return schema, compileErr
}

// Validate reports whether data is a valid recipe record. The returned error
// lists every schema violation.
func Validate(data []byte) error {
	s, err := compiled()
	if err != nil {
		return err
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("not JSON: %w", err)
	}
	return s.Validate(inst)
}
