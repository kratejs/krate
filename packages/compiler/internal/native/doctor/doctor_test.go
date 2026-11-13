package doctor

import (
	"bytes"
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

func TestMissingShellAndBrokenShell(t *testing.T) {
	m := &fakeMachine{goos: "linux"}
	r := Run(Inputs{}, m.probes())
	if s := find(t, r, "krate-shell"); s.Status != Fail || !strings.Contains(s.Hint, "@krate/core") {
		t.Errorf("missing shell: %+v", s)
	}
	if w := find(t, r, "webview"); w.Status != Warn {
		t.Errorf("webview without shell: %+v", w)
	}
	if len(r.Failed()) != 1 {
		t.Errorf("failed = %+v", r.Failed())
	}

	m = &fakeMachine{goos: "linux", shell: "/s", shellErr: errors.New("the shell binary /x does not exist")}
	if s := find(t, Run(Inputs{}, m.probes()), "krate-shell"); s.Status != Fail || !strings.Contains(s.Detail, "does not exist") {
		t.Errorf("bad path: %+v", s)
	}

	m = &fakeMachine{goos: "linux", shell: "/s", infoErr: errors.New("exec format error")}
	r = Run(Inputs{}, m.probes())
	if w := find(t, r, "webview"); w.Status != Fail || !strings.Contains(w.Detail, "exec format error") {
		t.Errorf("shell that cannot run: %+v", w)
	}
}

func TestWebviewMissingIsRequired(t *testing.T) {
	m := &fakeMachine{goos: "linux", shell: "/s", info: linuxInfo(false)}
	r := Run(Inputs{}, m.probes())
	w := find(t, r, "WebKitGTK 4.1")
	if w.Status != Fail || !strings.Contains(w.Hint, "libwebkit2gtk-4.1-0") {
		t.Errorf("webview: %+v", w)
	}
	var buf bytes.Buffer
	Format(&buf, r)
	for _, want := range []string{"MISSING", "hint: apt install libwebkit2gtk-4.1-0", "1 required item(s) missing: WebKitGTK 4.1"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, buf.String())
		}
	}
}

func TestNoDisplayIsAWarning(t *testing.T) {
	info := linuxInfo(true)
	info.Display.Available, info.Display.Detail = false, "neither WAYLAND_DISPLAY nor DISPLAY is set"
	m := &fakeMachine{goos: "linux", shell: "/s", info: info}
	r := Run(Inputs{}, m.probes())
	if d := find(t, r, "display"); d.Status != Warn {
		t.Errorf("display: %+v", d)
	}
	if len(r.Failed()) != 0 {
		t.Errorf("a missing display must not fail the report: %+v", r.Failed())
	}
}

func TestHostRuntimes(t *testing.T) {
	m := &fakeMachine{goos: "linux", shell: "/s", info: linuxInfo(true),
		tools:    map[string]string{"node": "/usr/bin/node", "bun": "/usr/bin/bun"},
		versions: map[string]string{"/usr/bin/node": "v22.4.1\n", "/usr/bin/bun": "1.2.3\n"},
		env:      map[string]string{}}
	r := Run(Inputs{HostRuntime: "node", HostVersion: "22", Islands: []string{"bun", "deno"}}, m.probes())
	if n := find(t, r, "node"); n.Status != OK || !strings.Contains(n.Detail, "22.4.1") {
		t.Errorf("node: %+v", n)
	}
	if b := find(t, r, "bun"); b.Status != OK {
		t.Errorf("bun: %+v", b)
	}
	if d := find(t, r, "deno"); d.Status != Fail || !strings.Contains(d.Hint, "KRATE_DENO") || !strings.Contains(d.Detail, "native.host.islands") {
		t.Errorf("deno: %+v", d)
	}
	// A pinned version that does not match.
	r = Run(Inputs{HostRuntime: "node", HostVersion: "20"}, m.probes())
	if n := find(t, r, "node"); n.Status != Fail || !strings.Contains(n.Detail, "native.host.version") {
		t.Errorf("version pin: %+v", n)
	}
	// KRATE_NODE wins over PATH.
	m.env["KRATE_NODE"] = "/opt/node/bin/node"
	m.versions["/opt/node/bin/node"] = "v20.1.0"
	r = Run(Inputs{HostRuntime: "node", HostVersion: "20"}, m.probes())
	if n := find(t, r, "node"); n.Status != OK || !strings.Contains(n.Detail, "KRATE_NODE") {
		t.Errorf("KRATE_NODE: %+v", n)
	}
}

func TestScriptc(t *testing.T) {
	m := &fakeMachine{goos: "linux", shell: "/s", info: linuxInfo(true), tools: map[string]string{"scriptc": "/bin/scriptc"}, env: map[string]string{}}
	r := Run(Inputs{HostRuntime: "scriptc"}, m.probes())
	if l := find(t, r, "scriptc linker"); l.Status != Fail || !strings.Contains(l.Hint, "SCRIPTC_LINKER") {
		t.Errorf("linker: %+v", l)
	}
	m.tools["clang"] = "/usr/bin/clang"
	if l := find(t, Run(Inputs{HostRuntime: "scriptc"}, m.probes()), "scriptc linker"); l.Status != OK {
		t.Errorf("linker: %+v", l)
	}
	delete(m.tools, "scriptc")
	if s := find(t, Run(Inputs{HostRuntime: "scriptc"}, m.probes()), "scriptc"); s.Status != Fail {
		t.Errorf("scriptc: %+v", s)
	}
}

func TestPackagingToolsPerOS(t *testing.T) {
	linux := &fakeMachine{goos: "linux", shell: "/s", info: linuxInfo(true)}
	r := Run(Inputs{LinuxFormats: []string{"appimage"}}, linux.probes())
	if a := find(t, r, "appimagetool"); a.Status != Fail {
		t.Errorf("appimagetool required when configured: %+v", a)
	}
	if a := find(t, Run(Inputs{}, linux.probes()), "appimagetool"); a.Status != Skip {
		t.Errorf("appimagetool optional by default: %+v", a)
	}

	mac := &fakeMachine{goos: "darwin", shell: "/s", info: &ShellInfo{Schema: 1, OS: "darwin", Arch: "amd64"}, tools: map[string]string{"codesign": "/usr/bin/codesign", "xcrun": "/usr/bin/xcrun"}}
	mac.info.Webview.Name, mac.info.Webview.OK, mac.info.Webview.Version = "WKWebView", true, "14.5"
	mac.info.Display.Available = true
	r = Run(Inputs{}, mac.probes())
	if h := find(t, r, "hdiutil"); h.Status != Fail {
		t.Errorf("hdiutil: %+v", h)
	}
	if c := find(t, r, "codesign"); c.Status != OK {
		t.Errorf("codesign: %+v", c)
	}

	win := &fakeMachine{goos: "windows", shell: `C:\k\krate-shell.exe`, tools: map[string]string{"signtool": `C:\sdk\signtool.exe`}}
	win.info = &ShellInfo{Schema: 1, OS: "windows", Arch: "amd64", OSVersion: "10.0.22631"}
	win.info.Webview.Name, win.info.Webview.OK, win.info.Webview.Version = "WebView2", true, "126.0.2592.87"
	win.info.Display.Available = true
	r = Run(Inputs{WindowsInstaller: "nsis"}, win.probes())
	if n := find(t, r, "makensis"); n.Status != Fail {
		t.Errorf("makensis with nsis: %+v", n)
	}
	if s := find(t, r, "signtool"); s.Status != OK {
		t.Errorf("signtool: %+v", s)
	}
	if w := find(t, r, "WebView2"); w.Status != OK || !strings.Contains(w.Detail, "126.0.2592.87") || !strings.Contains(w.Detail, "Windows") {
		t.Errorf("webview2: %+v", w)
	}
	if r := Run(Inputs{WindowsInstaller: "msix"}, win.probes()); len(r.Failed()) != 0 {
		t.Errorf("msix needs no tool: %+v", r.Failed())
	}
}

func TestShellForAnotherTargetWarns(t *testing.T) {
	info := linuxInfo(true)
	info.Arch = "arm64"
	m := &fakeMachine{goos: "linux", shell: "/s", info: info}
	if w := find(t, Run(Inputs{}, m.probes()), "krate-shell target"); w.Status != Warn {
		t.Errorf("target: %+v", w)
	}
}

func TestParseShellInfo(t *testing.T) {
	s, err := ParseShellInfo([]byte(`{"schema":1,"shell":"krate-shell","os":"linux","arch":"amd64","webview":{"name":"WebKitGTK 4.1","present":true,"version":"2.54.0","ok":true},"display":{"available":true,"detail":"x11 (:97)"}}`))
	if err != nil || s.Webview.Version != "2.54.0" || !s.Display.Available {
		t.Fatalf("%+v %v", s, err)
	}
	if _, err := ParseShellInfo([]byte("not json")); err == nil {
		t.Error("garbage accepted")
	}
	if _, err := ParseShellInfo([]byte(`{"os":"linux"}`)); err == nil {
		t.Error("report without a schema accepted")
	}
}

func TestGoRequiredForPlugins(t *testing.T) {
	m := &fakeMachine{goos: "linux", shell: "/s", info: linuxInfo(true), tools: map[string]string{}}
	in := Inputs{Plugins: []string{"./native-plugins/system-info"}, Compile: "never"}
	r := Run(in, m.probes())
	g := find(t, r, "Go")
	if g.Status != Fail || g.Hint == "" || !strings.Contains(g.Detail, "native plugins") {
		t.Errorf("plugins without go: %+v", g)
	}
	// Plugins win over native.release.compile "never": they cannot be linked without go.
	m.tools["go"] = "/usr/local/go/bin/go"
	m.versions = map[string]string{"/usr/local/go/bin/go": "go version go1.26.1 linux/amd64\n"}
	if g := find(t, Run(in, m.probes()), "Go"); g.Status != OK || !strings.Contains(g.Detail, "plugins") {
		t.Errorf("plugins with go: %+v", g)
	}
}
