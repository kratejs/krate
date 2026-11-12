package shell

import (
	"path"
	"path/filepath"

	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/extension"
)

// declareExtensions tells the extension area which directories the app
// packages, by name.
func (a *app) declareExtensions(man *Manifest) {
	var list []extension.Declared
	for _, e := range man.Extensions {
		list = append(list, extension.Declared{Name: path.Base(e), Dir: filepath.Join(a.childBase(), filepath.FromSlash(e))})
	}
	extension.Configure(list)
}

// startAutomationAndExtensions runs once the first window exists: it reports
// the debugging endpoint and loads the declared extensions.
func (a *app) startAutomationAndExtensions(p platform.App) {
	if au, ok := p.(caps.Automation); ok {
		if ep := au.AutomationEndpoint(); ep.Kind != "" {
			a.logf("automation endpoint (%s): %s", ep.Kind, ep.URL)
		}
	}
	if len(a.man.Extensions) == 0 {
		return
	}
	go func() {
		for name, err := range extension.LoadDeclared(p) {
			a.logf("shell: extension %s: %v", name, err)
		}
	}()
}
