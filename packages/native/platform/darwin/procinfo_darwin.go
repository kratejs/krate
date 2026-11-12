//go:build darwin

package darwin

import (
	"github.com/ebitengine/purego/objc"

	"github.com/kratejs/krate/packages/native/memstat"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.ProcessInfo = (*App)(nil)

// WebviewProcesses names the WebContent, Networking and GPU processes. They
// are XPC services, not children of this process, so they are found through
// private WebKit selectors, each guarded with respondsToSelector:
// -[WKWebView _webProcessIdentifier] and _gpuProcessIdentifier per window, and
// _networkProcessIdentifier on the web view's website data store or process
// pool. A selector that is missing leaves that process out and adds a note.
// The macOS CI memory report lists the webview:web and webview:network roles
// from it.
func (a *App) WebviewProcesses() ([]caps.ProcessRef, []string) {
	var refs []caps.ProcessRef
	var notes []string
	seen := map[int]bool{}
	add := func(pid int32, role string) {
		if pid > 0 && !seen[int(pid)] {
			seen[int(pid)] = true
			refs = append(refs, caps.ProcessRef{PID: int(pid), Role: role})
		}
	}
	runOnMain(func() {
		withPool(func() {
			var webs []objc.ID
			for _, w := range a.windows {
				for _, v := range w.liveViews() {
					webs = append(webs, v.web)
				}
			}
			for _, web := range webs {
				if s := sel("_webProcessIdentifier"); respondsTo(web, s) {
					add(objc.Send[int32](web, s), memstat.RoleWebviewWeb)
				} else {
					notes = append(notes, "WKWebView does not answer _webProcessIdentifier; web content processes are not attributable")
				}
				if s := sel("_gpuProcessIdentifier"); respondsTo(web, s) {
					add(objc.Send[int32](web, s), memstat.RoleWebviewGPU)
				}
				found := false
				cfg := web.Send(sel("configuration"))
				for _, owner := range []objc.ID{cfg.Send(sel("websiteDataStore")), cfg.Send(sel("processPool"))} {
					if owner == 0 {
						continue
					}
					if s := sel("_networkProcessIdentifier"); respondsTo(owner, s) {
						add(objc.Send[int32](owner, s), memstat.RoleWebviewNetwork)
						found = true
						break
					}
				}
				if !found {
					notes = append(notes, "the networking process is not attributable on this macOS version")
				}
			}
		})
	})
	return refs, dedupe(notes)
}

func dedupe(s []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, x := range s {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
