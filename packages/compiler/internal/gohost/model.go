package gohost

import "github.com/kratejs/krate/packages/compiler/internal/gotsgen"

// Route is one compiled Go API route.
type Route struct {
	ID         string
	Path       string // Go ServeMux pattern, e.g. "/api/users/{id}"
	Methods    []string
	Source     string
	ImportPath string
	Register   string
}

// RouteEntry is one route in the manifest read by `krate serve`.
type RouteEntry struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

// Manifest is the JSON file the main server reads to know which /api routes the
// Go host handles.
type Manifest struct {
	Routes []RouteEntry `json:"routes"`
}

// ComponentFile pairs a source file with its parsed components (for typegen).
type ComponentFile struct {
	Path  string
	Comps []gotsgen.Component
}

// ComponentPackage is one generated Go package: a source directory and all its
// .go files, so multi-file component packages compile together.
type ComponentPackage struct {
	Rel   string // slash-rel dir under components/ ("" for a root dir)
	Dir   string
	Pkg   string
	Files []string
	Comps []gotsgen.Component
}
