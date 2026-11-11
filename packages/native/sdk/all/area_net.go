//go:build !krate_no_net

package all

import (
	"github.com/kratejs/krate/packages/native/scheme"
	sdknet "github.com/kratejs/krate/packages/native/sdk/net"
	"github.com/kratejs/krate/packages/native/sdk/permission"
)

func init() {
	dataAreas = append(dataAreas, func(r *scheme.Registry, set permission.Set) { sdknet.Register(r, set) })
}
