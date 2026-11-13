package doctor

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeMachine is a set of injected probes: tools maps an executable name to
// its path, versions maps a path to the output of its version command.
type fakeMachine struct {
	goos     string
	tools    map[string]string
	versions map[string]string
	env      map[string]string
	shell    string
	shellErr error
	info     *ShellInfo
	infoErr  error
}

func (m *fakeMachine) probes() Probes {
	return Probes{
		GOOS: m.goos, GOARCH: "amd64",
		Getenv: func(k string) string { return m.env[k] },
		LookPath: func(n string) (string, error) {
			if p, ok := m.tools[n]; ok {
				return p, nil
			}
			return "", errors.New("not found")
		},
		Output: func(name string, args ...string) (string, error) {
			if v, ok := m.versions[name]; ok {
				return v, nil
			}
			return "", fmt.Errorf("cannot run %s", name)
		},
		FindShell:   func() (string, string, error) { return m.shell, "next to the krate executable", m.shellErr },
		ShellDoctor: func(string) (*ShellInfo, error) { return m.info, m.infoErr },
	}
}

func linuxInfo(ok bool) *ShellInfo {
	var s ShellInfo
	s.Schema, s.OS, s.Arch, s.OSVersion = 1, "linux", "amd64", "Debian GNU/Linux 13"
	s.Webview.Name, s.Webview.Present, s.Webview.Version, s.Webview.MinVersion = "WebKitGTK 4.1", true, "2.54.0", "2.40"
	s.Webview.OK = ok
	s.Webview.Libraries = []string{"/usr/lib/libwebkit2gtk-4.1.so.0.24.3"}
	if !ok {
		s.Webview.Present, s.Webview.Version = false, ""
		s.Webview.Error = "cannot load libwebkit2gtk-4.1.so.0"
		s.Webview.Hint = "apt install libwebkit2gtk-4.1-0"
	}
	s.Display.Available, s.Display.Detail = true, "x11 (:0)"
	return &s
}

func find(t *testing.T, r Report, name string) Item {
	t.Helper()
	for _, it := range r.Items {
		if it.Name == name {
			return it
		}
	}
	t.Fatalf("no item %q in %+v", name, r.Items)
	return Item{}
}

func TestHealthyTypeScriptOnlyLinuxWithoutGo(t *testing.T) {
	m := &fakeMachine{goos: "linux", shell: "/opt/krate/krate-shell", info: linuxInfo(true), tools: map[string]string{"dpkg-deb": "/usr/bin/dpkg-deb"}}
	r := Run(Inputs{}, m.probes())
	if f := r.Failed(); len(f) != 0 {
		t.Fatalf("failed: %+v", f)
	}
	if g := find(t, r, "Go"); g.Status != Skip {
		t.Errorf("Go without go host code must be optional: %+v", g)
	}
	if w := find(t, r, "WebKitGTK 4.1"); w.Status != OK || !strings.Contains(w.Detail, "2.54.0") || !strings.Contains(w.Detail, "2.40") || len(w.Paths) != 1 {
		t.Errorf("webview: %+v", w)
	}
	if q := find(t, r, "quickjs"); q.Status != OK {
		t.Errorf("quickjs: %+v", q)
	}
	if d := find(t, r, "dpkg-deb"); d.Status != OK {
		t.Errorf("dpkg-deb: %+v", d)
	}
}

func TestGoRequiredForGoHostAndCompileAlways(t *testing.T) {
	base := func() *fakeMachine {
		return &fakeMachine{goos: "linux", shell: "/s", info: linuxInfo(true), tools: map[string]string{}}
	}
	for name, in := range map[string]Inputs{
		"go host":        {GoHost: true},
		"compile always": {Compile: "always"},
	} {
		r := Run(in, base().probes())
		if g := find(t, r, "Go"); g.Status != Fail || g.Hint == "" {
			t.Errorf("%s: %+v", name, g)
		}
	}
	// compile never: Go is not used.
	r := Run(Inputs{Compile: "never"}, base().probes())
	if g := find(t, r, "Go"); g.Status != Skip || !strings.Contains(g.Detail, "never") {
		t.Errorf("never: %+v", g)
	}
	// With Go installed the version shows.
	m := base()
	m.tools["go"] = "/usr/local/go/bin/go"
	m.versions = map[string]string{"/usr/local/go/bin/go": "go version go1.26.1 linux/amd64\n"}
	g := find(t, Run(Inputs{GoHost: true}, m.probes()), "Go")
	if g.Status != OK || !strings.Contains(g.Detail, "go1.26.1") {
		t.Errorf("go present: %+v", g)
	}
}
