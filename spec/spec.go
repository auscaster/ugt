// Package spec embeds UGT's published JSON Schemas so every consumer validates
// against the same bytes that are documented in this directory.
package spec

import _ "embed"

// RecipeV0URL is the $id of the draft recipe schema.
const RecipeV0URL = "https://github.com/auscaster/ugt/spec/recipe/v0/recipe.schema.json"

// RecipeV0 is the draft recipe schema; recipe_format names the current draft
// ("ugt-draft-4") and changes with each breaking revision until v1.
//
//go:embed recipe/v0/recipe.schema.json
var RecipeV0 []byte

// CatalogueV0URL is the $id of the catalogue API's schema.
const CatalogueV0URL = "https://github.com/auscaster/ugt/spec/catalogue/v0/catalogue.schema.json"

// CatalogueV0 is the schema of what the catalogue service answers.
//
//go:embed catalogue/v0/catalogue.schema.json
var CatalogueV0 []byte

// AdmissionV0URL is the $id of the admission record's schema.
const AdmissionV0URL = "https://github.com/auscaster/ugt/spec/admission/v0/admission.schema.json"

// AdmissionV0 is the schema of what a verification run reports to the
// catalogue service.
//
//go:embed admission/v0/admission.schema.json
var AdmissionV0 []byte
