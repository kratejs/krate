package fakeplat

import "github.com/kratejs/krate/packages/native/platform/caps"

var (
	_ caps.OpenFiles = (*App)(nil)
	_ caps.FileDrops = (*App)(nil)
	_ caps.OpenURLs  = (*App)(nil)
)

// OnOpenFiles implements caps.OpenFiles.
func (a *App) OnOpenFiles(fn func([]string)) { a.mu.Lock(); a.openFn = fn; a.mu.Unlock() }

// EmitOpenFiles delivers an open-file request as the platform would.
func (a *App) EmitOpenFiles(paths ...string) {
	a.mu.Lock()
	fn := a.openFn
	a.mu.Unlock()
	if fn != nil {
		fn(paths)
	}
}

// OnFileDrop implements caps.FileDrops.
func (a *App) OnFileDrop(fn func(caps.FileDrop)) { a.mu.Lock(); a.dropFn = fn; a.mu.Unlock() }

// EmitFileDrop delivers a drop onto a view as the platform would.
func (a *App) EmitFileDrop(d caps.FileDrop) {
	a.mu.Lock()
	fn := a.dropFn
	a.mu.Unlock()
	if fn != nil {
		fn(d)
	}
}

// OnOpenURLs implements caps.OpenURLs.
func (a *App) OnOpenURLs(fn func([]string)) { a.mu.Lock(); a.urlFn = fn; a.mu.Unlock() }

// EmitOpenURLs delivers URLs of the app's schemes as macOS would.
func (a *App) EmitOpenURLs(urls ...string) {
	a.mu.Lock()
	fn := a.urlFn
	a.mu.Unlock()
	if fn != nil {
		fn(urls)
	}
}
