// Package archtest enforces the helper's import rules (PLAN.md, system design):
// the domain depends on nothing outside itself, app only on the domain,
// adapters and surfaces only inward, and only cmd/ugt wires them together.
package archtest

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Module is the Go module path of this repository.
const Module = "github.com/auscaster/ugt"

// External libraries the domain may use: schema and YAML parsing only.
var domainLibraries = []string{
	"github.com/santhosh-tekuri/jsonschema/",
	"gopkg.in/yaml.v3",
}

// Standard packages that reach the world: the domain and app never import
// them outside tests. They reach it only through app's ports, so the core
// runs the same against fakes as against Windows, macOS and Linux.
var world = map[string]bool{
	"os": true, "os/exec": true, "os/signal": true, "os/user": true,
	"net": true, "net/http": true, "syscall": true,
}

// Layer names a package's place in the hexagon.
func Layer(pkg string) string {
	rel := strings.TrimPrefix(pkg, Module+"/")
	switch {
	case strings.HasPrefix(rel, "internal/domain/"):
		return "domain"
	case rel == "internal/app" || strings.HasPrefix(rel, "internal/app/"):
		return "app"
	case strings.HasPrefix(rel, "internal/adapter/"):
		return "adapter"
	case strings.HasPrefix(rel, "internal/surface/"):
		return "surface"
	case strings.HasPrefix(rel, "internal/devtools/"):
		return "devtools"
	case rel == "spec" || strings.HasPrefix(rel, "spec/"):
		return "spec"
	case rel == "recipes":
		return "recipes"
	case rel == "layer":
		return "layer"
	case rel == "skills":
		return "skills"
	case strings.HasPrefix(rel, "cmd/"):
		return "cmd"
	}
	return "unknown"
}

// adapterName is the adapter a package belongs to: internal/adapter/<name>/...
func adapterName(pkg string) string {
	rest := strings.TrimPrefix(pkg, Module+"/internal/adapter/")
	name, _, _ := strings.Cut(rest, "/")
	return name
}

// Check returns one message per import that breaks the rules, for the module
// rooted at root. Test files are included: tests must respect the layers too.
func Check(root string) ([]string, error) {
	imports, err := moduleImports(root)
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, pkg := range sortedKeys(imports) {
		layer := Layer(pkg)
		if layer == "unknown" {
			problems = append(problems, fmt.Sprintf("%s is outside the locked layout", pkg))
			continue
		}
		for _, imp := range imports[pkg] {
			msg := rule(pkg, layer, imp.path)
			if msg == "" && !imp.test {
				msg = worldRule(layer, imp.path)
			}
			if msg != "" {
				problems = append(problems, fmt.Sprintf("%s imports %s: %s", pkg, imp.path, msg))
			}
		}
	}
	return problems, nil
}

func rule(pkg, layer, imp string) string {
	internal := strings.HasPrefix(imp, Module+"/")
	if !internal {
		if layer == "domain" && !isStdlib(imp) && !hasAnyPrefix(imp, domainLibraries) {
			return "the domain may use only the standard library and the schema and YAML libraries"
		}
		return ""
	}
	target := Layer(imp)
	if target == "devtools" && layer != "devtools" {
		return "nothing shipped imports devtools"
	}
	if target == "recipes" && layer != "cmd" && layer != "devtools" {
		return "only the composition root reads the built-in catalogue, through the Catalogue port"
	}
	if target == "layer" && layer != "cmd" {
		return "only the composition root reads the layer contract, and hands it to the runtime adapter"
	}
	if target == "skills" && layer != "cmd" && layer != "devtools" {
		return "only the composition root reads the built-in skill, and hands it to the agents adapter"
	}
	switch layer {
	case "domain":
		if target != "domain" && target != "spec" {
			return "the domain imports only the domain and spec"
		}
	case "app":
		if target != "domain" && target != "spec" && target != "app" {
			return "app imports only the domain and spec"
		}
	case "adapter":
		if target == "surface" || target == "cmd" {
			return "adapters never import surfaces"
		}
		if target == "adapter" && adapterName(imp) != adapterName(pkg) {
			return "an adapter never imports another adapter"
		}
	case "surface":
		if target == "adapter" || target == "cmd" {
			return "surfaces reach adapters only through app's ports"
		}
	case "spec":
		if target != "spec" {
			return "spec holds published contracts and imports nothing internal"
		}
	case "recipes":
		if target != "domain" {
			return "the catalogue is data; its test reads it through the domain only"
		}
	case "layer", "skills":
		return "the layer contract and the skill are data and import nothing internal"
	}
	return ""
}

// worldRule applies to shipped code only: tests may use the real disk.
func worldRule(layer, imp string) string {
	if (layer == "domain" || layer == "app") && world[imp] {
		return layer + " reaches the operating system and network only through app's ports"
	}
	return ""
}

// importRef is one import and whether a test file makes it.
type importRef struct {
	path string
	test bool
}

func moduleImports(root string) (map[string][]importRef, error) {
	imports := map[string][]importRef{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg := Module
		if rel != "." {
			pkg += "/" + filepath.ToSlash(rel)
		}
		for _, spec := range f.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			imports[pkg] = append(imports[pkg], importRef{imp, strings.HasSuffix(path, "_test.go")})
		}
		if _, ok := imports[pkg]; !ok {
			imports[pkg] = nil
		}
		return nil
	})
	return imports, err
}

// isStdlib reports whether an import path belongs to the standard library:
// its first element has no dot.
func isStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string][]importRef) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
