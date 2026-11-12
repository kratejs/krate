// Package all wires the whole Krate Native SDK into a scheme.Registry. The
// shell calls Register once per app.
package all

import (
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk"
	sdkos "github.com/kratejs/krate/packages/native/sdk/os"
	"github.com/kratejs/krate/packages/native/sdk/permission"
)

// Options identify the app and its permissions.
type Options struct {
	// AppID, Name and Version come from the manifest. AppID is required and
	// names every per-app directory and credential.
	AppID   string
	Name    string
	Version string
	// Permissions is the app.json "permissions" list; empty means defaults.
	Permissions []string
	// Strict makes an empty Permissions list mean "nothing".
	Strict bool
	// UpdaterURL and UpdaterPublicKey are native.updater from app.json; both
	// empty leaves the updater ids answering "unsupported".
	UpdaterURL       string
	UpdaterPublicKey string
	// UpdaterChannel, UpdaterMaxStarts, UpdaterHealthTimeout (seconds) and
	// UpdaterManualConfirm are the other native.updater keys.
	UpdaterChannel       string
	UpdaterMaxStarts     int
	UpdaterHealthTimeout int
	UpdaterManualConfirm bool
}

// Register configures the SDK for the app and registers every UI id on r.
// An id whose permission is not granted is still registered, as a stub that
// fails with code "permission_denied", so the UI can tell a missing
// permission from a typo. It returns the parsed permission set.
func Register(r *scheme.Registry, o Options) (permission.Set, error) {
	var set permission.Set
	var err error
	if o.Strict && len(o.Permissions) == 0 {
		set = permission.None()
	} else {
		set, err = permission.Parse(o.Permissions)
	}
	if err != nil {
		return permission.Set{}, err
	}
	if err := sdkos.Configure(sdkos.App{ID: o.AppID, Name: o.Name, Version: o.Version}); err != nil {
		return permission.Set{}, err
	}
	if configureUpdater != nil {
		configureUpdater(o)
	}
	RegisterAreas(r, set)
	return set, nil
}

// RegisterUI exposes the UI half of the SDK (window, dialog, clipboard, menu,
// tray, shortcut, notify, app) of an attached Host on r under the same
// permission set as the data areas. A call to an id whose permission is
// missing fails with "permission_denied" naming it. override, when non-nil,
// replaces set.Allows (tests of the shell use it).
func RegisterUI(r *scheme.Registry, h *sdk.Host, set permission.Set, override func(permission string) bool) {
	allowed := set.AllowsFor
	if override != nil {
		allowed = func(_, p string) bool { return override(p) }
	}
	h.SetWindowScope(set.ScopeChild)
	h.SetWindowUnscope(set.Unscope)
	h.RegisterUIFor(r, allowed, permission.Denied)
}

// RegisterAreas registers every area on r without configuring the app
// identity (Register does that). Tests use it with a Set of their choice.
func RegisterAreas(r *scheme.Registry, set permission.Set) {
	permission.RegisterIn(r, set)
	sdkos.Register(r, set)
	for _, a := range dataAreas {
		a(r, set)
	}
}

// dataAreas are the optional data areas (fs, db, kv, net, updater). Each registers
// itself from init in area_<name>.go behind the build tag !krate_no_<name>.
var dataAreas []func(*scheme.Registry, permission.Set)

// configureUpdater is set by area_updater.go; nil when the updater area is
// built out (build tag krate_no_updater).
var configureUpdater func(Options)
