// Package recipes is the catalogue as of this commit. It is built into ugt,
// so finding and running a recipe works offline and before the published
// index; recipes made on the machine come first.
package recipes

import "embed"

// FS holds every recipe folder, as <owner>/<repo>/<task>/.
//
//go:embed all:*/*/*
var FS embed.FS
