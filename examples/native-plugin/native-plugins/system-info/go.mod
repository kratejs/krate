module github.com/kratejs/krate-plugin-system-info

go 1.26.1

require github.com/kratejs/krate/packages/native v0.0.0

require (
	github.com/ebitengine/purego v0.11.3 // indirect
	golang.org/x/sys v0.48.0 // indirect
)

// Inside the Krate monorepo the native module is the sibling package. A
// published plugin drops this line and requires a released version:
// go get github.com/kratejs/krate/packages/native
replace github.com/kratejs/krate/packages/native => ../../../../packages/native
