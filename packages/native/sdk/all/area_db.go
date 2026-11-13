//go:build !krate_no_db

package all

import (
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/db"
	"github.com/kratejs/krate/packages/native/sdk/permission"
)

func init() {
	dataAreas = append(dataAreas, func(r *scheme.Registry, set permission.Set) { db.Register(r, set) })
}
