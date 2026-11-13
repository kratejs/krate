//go:build linux

package linux

import (
	"fmt"
	"os"

	"github.com/kratejs/krate/packages/native/memstat"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.ProcessInfo = (*App)(nil)

// WebviewProcesses lists the WebKit helper processes (WebKitWebProcess,
// WebKitNetworkProcess, WebKitGPUProcess) among the descendants of this
// process, classified by executable name.
func (a *App) WebviewProcesses() ([]caps.ProcessRef, []string) {
	kids, err := memstat.Descendants(os.Getpid())
	if err != nil {
		return nil, []string{fmt.Sprintf("listing child processes: %v", err)}
	}
	var refs []caps.ProcessRef
	for _, pid := range kids {
		if role := memstat.WebKitRole(memstat.Name(pid)); role != "" {
			refs = append(refs, caps.ProcessRef{PID: pid, Role: role})
		}
	}
	return refs, nil
}
