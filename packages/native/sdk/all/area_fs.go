//go:build !krate_no_fs

package all

import (
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/fs"
	"github.com/kratejs/krate/packages/native/sdk/permission"
)

func init() {
	dataAreas = append(dataAreas, func(r *scheme.Registry, set permission.Set) { fs.Register(r, set) })
}
