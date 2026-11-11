//go:build !krate_no_kv

package all

import (
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/kv"
	"github.com/kratejs/krate/packages/native/sdk/permission"
)

func init() {
	dataAreas = append(dataAreas, func(r *scheme.Registry, set permission.Set) { kv.Register(r, set) })
}
