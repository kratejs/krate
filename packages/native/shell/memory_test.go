package shell

import (
	"encoding/json"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/internal/gohosttest"
	"github.com/kratejs/krate/packages/native/memstat"
)

func memRoles(r *memstat.Report) map[string]memstat.Proc {
	m := map[string]memstat.Proc{}
	for _, p := range r.ByRole() {
		m[p.Role] = p
	}
	return m
}

func skipNoMemstat(t *testing.T) {
	t.Helper()
	switch runtime.GOOS {
	case "linux", "darwin", "windows":
	default:
		t.Skip("memstat has no implementation on " + runtime.GOOS)
	}
}

type fakeMemCtl struct {
	rep *memstat.Report
	err error
}

func (fakeMemCtl) ReloadHostJS() error  { return nil }
func (fakeMemCtl) RestartGoHost() error { return nil }
func (fakeMemCtl) Eval(string) error    { return nil }
func (fakeMemCtl) HMR(string) error     { return nil }
func (fakeMemCtl) Quit()                {}
func (f fakeMemCtl) Memory() (*memstat.Report, error) {
	return f.rep, f.err
}

type plainCtl struct{ fakeMemCtl }

// Memory is hidden: plainCtl does not satisfy MemorySampler.
func (plainCtl) Memory() {}

func TestServeControlMemory(t *testing.T) {
	rep := &memstat.Report{OS: "linux", Source: "smaps_rollup",
		Procs: []memstat.Proc{{PID: 7, Name: "krate-shell", Role: memstat.RoleShell, PSS: 1 << 20}}}
	var out strings.Builder
	ServeControl(strings.NewReader("memory\n"), &out, fakeMemCtl{rep: rep})
	line := strings.TrimSpace(out.String())
	if !strings.HasPrefix(line, "ok {") || strings.Contains(line, "\n") {
		t.Fatalf("reply %q", out.String())
	}
	var got memstat.Report
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "ok ")), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Procs) != 1 || got.Procs[0].PSS != 1<<20 || got.Source != "smaps_rollup" {
		t.Fatalf("decoded %+v", got)
	}

	out.Reset()
	ServeControl(strings.NewReader("memory\n"), &out, plainCtl{})
	if !strings.HasPrefix(out.String(), "error memory is not supported") {
		t.Fatalf("reply %q", out.String())
	}
}

// Browser mode has no window: the shell and the Go host child are the roles.
func TestMemoryBrowserMode(t *testing.T) {
	skipNoMemstat(t)
	root, cleanup, err := gohosttest.TempDir("shellmem-")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	app, err := buildTestApp(root, "<!doctype html><title>idx</title>hello", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	ctl, _ := runBrowser(t, app.dir, newReportSink())

	rep, err := ctl.Memory()
	if err != nil {
		t.Fatalf("Memory: %v", err)
	}
	roles := memRoles(rep)
	if len(roles) != 2 || roles[memstat.RoleShell].PSS == 0 || roles[memstat.RoleGoHost].PSS == 0 {
		t.Fatalf("roles in browser mode: %+v", rep.ByRole())
	}
	if rep.Total.PSS != roles[memstat.RoleShell].PSS+roles[memstat.RoleGoHost].PSS {
		t.Errorf("total %d is not the sum of the roles", rep.Total.PSS)
	}

	// The same report over HTTP, behind the session token.
	u, _ := url.Parse(ctl.URL())
	base := "http://" + u.Host
	jar := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if resp, _ := get(t, jar, base+devMemoryPath, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("memory without token: %d", resp.StatusCode)
	}
	tok := u.Query().Get("krate_token")
	resp, body := get(t, jar, base+devMemoryPath, func(r *http.Request) { r.Header.Set("X-Krate-Token", tok) })
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("memory: %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var got memstat.Report
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("%v\n%s", err, body)
	}
	if r := memRoles(&got); r[memstat.RoleShell].PSS == 0 || r[memstat.RoleGoHost].PSS == 0 || len(r) != 2 {
		t.Errorf("endpoint roles: %+v", got.ByRole())
	}
}
