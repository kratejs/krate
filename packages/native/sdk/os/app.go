// Package sdkos is the os area of the Krate Native SDK: per-app directories,
// OS information, opening URLs in the default handler and secrets in the OS
// credential store.
//
// Go host code calls these functions directly. UI and TypeScript host code
// reach the selected operations through Register (ids krate/os.*, gated by the
// permissions in package permission).
//
// Every function needs to know which app it works for. The shell calls
// Configure at startup; a Go host child process has no manifest, so when
// Configure was not called the identity comes from the KRATE_APP_ID,
// KRATE_APP_NAME and KRATE_APP_VERSION environment variables, which the shell
// sets for the child.
package sdkos

import (
	"fmt"
	"os"
	"regexp"
	"sync"
)

// App identifies the running app.
type App struct {
	// ID is the manifest appId; it names every directory and credential.
	ID string
	// Name is the product name (informational).
	Name string
	// Version is the app version (informational).
	Version string
}

var appIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

var (
	appMu sync.RWMutex
	app   *App
)

// Configure sets the app identity for this process. It rejects an id that is
// not safe to use as a directory name.
func Configure(a App) error {
	if !appIDRe.MatchString(a.ID) || a.ID == "." || a.ID == ".." {
		return fmt.Errorf("sdkos: appId %q is not a valid directory name", a.ID)
	}
	appMu.Lock()
	app = &a
	appMu.Unlock()
	return nil
}

// Current returns the configured app, or the one named by the environment.
func Current() (App, error) {
	appMu.RLock()
	a := app
	appMu.RUnlock()
	if a != nil {
		return *a, nil
	}
	id := os.Getenv("KRATE_APP_ID")
	if id == "" {
		return App{}, fmt.Errorf("sdkos: the app is not configured (call sdkos.Configure or set KRATE_APP_ID)")
	}
	if !appIDRe.MatchString(id) || id == "." || id == ".." {
		return App{}, fmt.Errorf("sdkos: KRATE_APP_ID %q is not a valid directory name", id)
	}
	return App{ID: id, Name: os.Getenv("KRATE_APP_NAME"), Version: os.Getenv("KRATE_APP_VERSION")}, nil
}
