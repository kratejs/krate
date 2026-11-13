package shell

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"

	"github.com/kratejs/krate/packages/native/memstat"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

const devMemoryPath = "/__krate/dev/memory"

// MemorySampler is the optional part of Controller behind the memory command.
type MemorySampler interface {
	Memory() (*memstat.Report, error)
}

// memoryReport samples the shell, the Go host child, the JavaScript host
// children and (in native mode, where the platform can name them) the webview
// processes. Processes that do not exist in this run (no window in browser
// mode, no child, a Go host linked into a release build) are simply absent.
func (a *app) memoryReport() (*memstat.Report, error) {
	targets := []memstat.Target{{PID: os.Getpid(), Role: memstat.RoleShell}}
	var notes []string

	a.mu.Lock()
	if a.child != nil {
		if pid := a.child.Pid(); pid > 0 {
			targets = append(targets, memstat.Target{PID: pid, Role: memstat.RoleGoHost})
		}
	}
	entries := make([]string, 0, len(a.hostChildren))
	for e := range a.hostChildren {
		entries = append(entries, e)
	}
	sort.Strings(entries)
	for _, e := range entries {
		hc := a.hostChildren[e]
		if pid := hc.child.Pid(); pid > 0 {
			targets = append(targets, memstat.Target{PID: pid, Role: memstat.HostChildRole(hc.spec.Runtime)})
		}
	}
	a.mu.Unlock()

	if p := a.plat.Load(); p != nil {
		if info, ok := (*p).(caps.ProcessInfo); ok {
			refs, n := info.WebviewProcesses()
			for _, r := range refs {
				targets = append(targets, memstat.Target{PID: r.PID, Role: r.Role})
			}
			notes = append(notes, n...)
		} else {
			notes = append(notes, "webview processes not attributable: the platform cannot name them")
		}
	}
	return memstat.Collect(targets, notes...)
}

// Memory samples the memory of the app's processes (see memstat for the
// numbers). It is the memory dev control command.
func (c *Control) Memory() (*memstat.Report, error) { return c.a.memoryReport() }

func (a *app) serveMemory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rep, err := a.memoryReport()
	if rep == nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(rep)
}
