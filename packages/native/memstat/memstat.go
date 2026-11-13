// Package memstat measures the memory of processes without cgo.
//
// Summing resident set sizes (RSS) over an app's processes counts every
// shared library once per process, which overstates the footprint by a large
// margin for a webview app. memstat reports the numbers that do not:
//
//   - PSS (proportional set size): private pages plus each shared page divided
//     by the number of processes that map it. Summed over processes it is the
//     real total.
//   - PrivateDirty: pages only this process has, that were written. It is
//     what the process costs the system that cannot be dropped without
//     swapping.
//
// What each OS can provide:
//
//	linux    /proc/<pid>/smaps_rollup (or the sum of /proc/<pid>/smaps): every
//	         field is exact.
//	darwin   proc_pid_rusage ri_phys_footprint, the number Activity Monitor
//	         shows. PSS and PrivateDirty are both set to the footprint (macOS
//	         has no per-page sharing split in that call); PrivateClean, Anon
//	         and Swap stay 0. RSS is ri_resident_size.
//	windows  GetProcessMemoryInfo: RSS is the working set, PSS and
//	         PrivateDirty are both PrivateUsage (committed private bytes; a
//	         private working set would need QueryWorkingSetEx, which walks
//	         every page). PrivateClean, Anon and Swap stay 0.
//
// All sizes are bytes. The darwin and windows code runs in the memory report
// of the full CI runs on macos-latest and windows-latest.
package memstat

import (
	"errors"
	"runtime"
	"sort"
	"strings"
)

// Roles of the processes of a Krate Native app.
const (
	RoleShell          = "shell"
	RoleGoHost         = "gohost"
	RoleHostChild      = "hostchild" // followed by ":<runtime>"
	RoleWebviewWeb     = "webview:web"
	RoleWebviewNetwork = "webview:network"
	RoleWebviewGPU     = "webview:gpu"
	RoleWebviewOther   = "webview:other"
)

// HostChildRole is the role of a JavaScript host child running on runtime.
func HostChildRole(runtime string) string { return RoleHostChild + ":" + runtime }

// Proc is the memory of one process, in bytes.
type Proc struct {
	PID          int    `json:"pid"`
	Name         string `json:"name"`
	Role         string `json:"role"`
	RSS          uint64 `json:"rss"`
	PSS          uint64 `json:"pss"`
	PrivateDirty uint64 `json:"privateDirty"`
	PrivateClean uint64 `json:"privateClean"`
	Anon         uint64 `json:"anon"`
	Swap         uint64 `json:"swap"`
}

// Add returns p with the memory of q added (PID, Name and Role are p's).
func (p Proc) Add(q Proc) Proc {
	p.RSS += q.RSS
	p.PSS += q.PSS
	p.PrivateDirty += q.PrivateDirty
	p.PrivateClean += q.PrivateClean
	p.Anon += q.Anon
	p.Swap += q.Swap
	return p
}

// Target names a process to sample and the role it plays.
type Target struct {
	PID  int
	Role string
}

// Source names where the numbers of this OS come from.
func Source() string {
	switch runtime.GOOS {
	case "linux":
		return "smaps_rollup"
	case "darwin":
		return "phys_footprint"
	case "windows":
		return "private_usage"
	}
	return "unsupported"
}

// Report is a sample of every process of an app.
type Report struct {
	OS     string `json:"os"`
	Source string `json:"source"`
	// Procs are the processes that could be read, in the order of the targets.
	Procs []Proc `json:"procs"`
	// Total sums Procs (PID 0, Role "total").
	Total Proc `json:"total"`
	// Notes say what could not be attributed or read.
	Notes []string `json:"notes,omitempty"`
}

// ErrUnsupported is returned by Sample on an OS without an implementation.
var ErrUnsupported = errors.New("memstat: not supported on " + runtime.GOOS)

// Collect samples targets and assigns their roles. A process that has exited
// is left out; other read errors become Notes. It fails only when nothing at
// all could be read.
func Collect(targets []Target, notes ...string) (*Report, error) {
	r := &Report{OS: runtime.GOOS, Source: Source(), Notes: append([]string(nil), notes...)}
	seen := map[int]bool{}
	for _, t := range targets {
		if t.PID <= 0 || seen[t.PID] {
			continue
		}
		seen[t.PID] = true
		ps, err := Sample(t.PID)
		if err != nil {
			if errors.Is(err, ErrUnsupported) {
				return nil, err
			}
			r.Notes = append(r.Notes, err.Error())
		}
		for _, p := range ps {
			p.Role = t.Role
			r.Procs = append(r.Procs, p)
		}
	}
	if len(r.Procs) == 0 && len(targets) > 0 {
		return r, errors.New("memstat: no process could be read")
	}
	r.Total = Proc{Role: "total"}
	for _, p := range r.Procs {
		r.Total = r.Total.Add(p)
	}
	return r, nil
}

// ByRole sums the processes of each role, sorted by role.
func (r *Report) ByRole() []Proc {
	m := map[string]Proc{}
	for _, p := range r.Procs {
		a := m[p.Role]
		a.Role = p.Role
		a.Name = p.Role
		m[p.Role] = a.Add(p)
	}
	out := make([]Proc, 0, len(m))
	for _, p := range m {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Role < out[j].Role })
	return out
}

// WebKitRole classifies a WebKitGTK helper process by its executable name. It
// returns "" for a name that is not a WebKit helper.
func WebKitRole(exe string) string {
	if i := strings.LastIndexAny(exe, `/\`); i >= 0 {
		exe = exe[i+1:]
	}
	exe = strings.TrimSuffix(strings.ToLower(exe), ".exe")
	switch {
	case strings.HasPrefix(exe, "webkitwebprocess"):
		return RoleWebviewWeb
	case strings.HasPrefix(exe, "webkitnetworkprocess"):
		return RoleWebviewNetwork
	case strings.HasPrefix(exe, "webkitgpuprocess"):
		return RoleWebviewGPU
	case strings.HasPrefix(exe, "webkit"):
		return RoleWebviewOther
	}
	return ""
}
