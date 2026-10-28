// Package goext exposes the embedded sources of the goext helper packages
// (krate, runtime) so the compiler can vendor them into a project's generated
// Go module under the same real import path:
//
//	github.com/kratejs/krate/packages/compiler/goext/krate
//	github.com/kratejs/krate/packages/compiler/goext/runtime
package goext

import _ "embed"

// KrateSrc is the source of the goext/krate helper package.
//
//go:embed krate/krate.go
var KrateSrc []byte

// KrateRequestSrc is the source of the goext/krate request-context file.
//
//go:embed krate/request.go
var KrateRequestSrc []byte

// KrateRevalidateSrc is the source of the goext/krate revalidation helpers.
//
//go:embed krate/revalidate.go
var KrateRevalidateSrc []byte

// RuntimeSrc is the source of the goext/runtime helper package.
//
//go:embed runtime/runtime.go
var RuntimeSrc []byte
