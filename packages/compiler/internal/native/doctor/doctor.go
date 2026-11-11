// Package doctor checks the machine for what Krate Native needs: the Go
// toolchain, the shell binary, the system webview, packaging and signing
// tools, and the host runtime the config selects. Every probe goes through
// Probes, so the report can be tested without the real machine.
package doctor

import (
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"sort"
	"strings"
)

// Status is the outcome of one check.
type Status string

const (
	// OK: found and usable.
	OK Status = "ok"
	// Fail: needed for this project's config and missing or unusable.
	Fail Status = "fail"
	// Warn: a problem that does not block building.
	Warn Status = "warn"
	// Skip: not found, and not needed for this project's config.
	Skip Status = "skip"
)

// Item is one line of the report.
type Item struct {
	Group  string `json:"group"`
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail,omitempty"`
	Hint   string `json:"hint,omitempty"`
	// Paths are files the check found (library paths), one per line.
	Paths []string `json:"paths,omitempty"`
}

// Report is the result of Run.
type Report struct {
	OS    string `json:"os"`
	Arch  string `json:"arch"`
	Items []Item `json:"items"`
}

// Failed returns the items with status Fail.
func (r *Report) Failed() []Item {
	var out []Item
	for _, it := range r.Items {
		if it.Status == Fail {
			out = append(out, it)
		}
	}
	return out
}

// Inputs are the facts of the project config that decide what is required.
type Inputs struct {
	// GoHost is true when the project has Go host code (compiled with go).
	GoHost bool
	// Plugins are the names of the native plugins (native.plugins); they are Go
	// code linked into a shell built for the app, so they need the Go toolchain.
	Plugins []string
	// Compile is native.release.compile: "auto", "always" or "never".
	Compile string
	// HostRuntime is native.host.runtime ("quickjs", "node", "bun", "deno",
	// "scriptc"); HostVersion pins node, bun and deno.
	HostRuntime string
	HostVersion string
	// Islands are the runtimes of native.host.islands.
	Islands []string
	// LinuxFormats is native.bundle.linux.formats (empty means deb).
	LinuxFormats []string
	// WindowsInstaller is native.bundle.windows.installer ("msix" or "nsis").
	WindowsInstaller string
}

// ShellInfo is what krate-shell --doctor prints (see packages/native/shell).
type ShellInfo struct {
	Schema    int    `json:"schema"`
	Shell     string `json:"shell"`
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	OSVersion string `json:"osVersion"`
	Webview   struct {
		Name       string   `json:"name"`
		Present    bool     `json:"present"`
		Version    string   `json:"version"`
		MinVersion string   `json:"minVersion"`
		OK         bool     `json:"ok"`
		Libraries  []string `json:"libraries"`
		Error      string   `json:"error"`
		Hint       string   `json:"hint"`
	} `json:"webview"`
	Display struct {
		Available bool   `json:"available"`
		Detail    string `json:"detail"`
	} `json:"display"`
}

// ParseShellInfo decodes the JSON line of krate-shell --doctor.
func ParseShellInfo(data []byte) (*ShellInfo, error) {
	var s ShellInfo
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("krate-shell --doctor printed something that is not JSON: %w", err)
	}
	if s.Schema == 0 {
		return nil, fmt.Errorf("krate-shell --doctor printed no schema version")
	}
	return &s, nil
}

// Probes are the system calls of the checks.
type Probes struct {
	GOOS, GOARCH string
	// Getenv reads an environment variable.
	Getenv func(string) string
	// LookPath finds an executable on PATH.
	LookPath func(name string) (string, error)
	// Output runs a command and returns its combined output.
	Output func(name string, args ...string) (string, error)
	// FindShell returns the shell binary and where it came from; "" when
	// there is none. A non-nil error is a path that was given and is wrong.
	FindShell func() (path, source string, err error)
	// ShellDoctor runs `<shell> --doctor`.
	ShellDoctor func(shell string) (*ShellInfo, error)
}

// Run checks the machine for a project with the given needs.
func Run(in Inputs, p Probes) Report {
	if p.GOOS == "" {
		p.GOOS = runtime.GOOS
	}
	if p.GOARCH == "" {
		p.GOARCH = runtime.GOARCH
	}
	if in.Compile == "" {
		in.Compile = "auto"
	}
	if in.HostRuntime == "" {
		in.HostRuntime = "quickjs"
	}
	r := Report{OS: p.GOOS, Arch: p.GOARCH}
	c := &checker{in: in, p: p, r: &r}
	c.goToolchain()
	c.shell()
	c.runtimes()
	c.packaging()
	return r
}

type checker struct {
	in Inputs
	p  Probes
	r  *Report
}

func (c *checker) add(group, name string, st Status, detail, hint string) {
	c.r.Items = append(c.r.Items, Item{Group: group, Name: name, Status: st, Detail: detail, Hint: hint})
}

// tool adds a line for an executable on PATH. need says whether the project
// config requires it; why explains the requirement or the optional use.
func (c *checker) tool(group, name, exe string, need bool, why string, versionArgs ...string) bool {
	path, err := c.p.LookPath(exe)
	if err != nil {
		st := Skip
		if need {
			st = Fail
		}
		c.add(group, name, st, "not found on PATH; "+why, "")
		return false
	}
	detail := path
	if len(versionArgs) > 0 {
		if out, err := c.p.Output(path, versionArgs...); err == nil {
			if v := firstLine(out); v != "" {
				detail = v + " (" + path + ")"
			}
		}
	}
	c.add(group, name, OK, detail, "")
	return true
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return s
}

func (c *checker) goToolchain() {
	need := c.in.GoHost || c.in.Compile == "always" || len(c.in.Plugins) > 0
	var why string
	switch {
	case len(c.in.Plugins) > 0:
		why = "the app uses native plugins (" + strings.Join(c.in.Plugins, ", ") + "), which are Go code linked into a shell that is built for the app with go"
	case c.in.GoHost:
		why = "the app has Go host code, which is compiled with go"
	case c.in.Compile == "always":
		why = `native.release.compile is "always"`
	case c.in.Compile == "never":
		why = "not used: native.release.compile is \"never\""
	default:
		why = "without it a release is the prebuilt-shell layout instead of one executable"
	}
	path, err := c.p.LookPath("go")
	if err != nil {
		st := Skip
		hint := ""
		if need {
			st = Fail
			hint = "install Go from https://go.dev/dl"
		}
		c.add("toolchain", "Go", st, "not found on PATH; "+why, hint)
		return
	}
	detail := path
	if out, err := c.p.Output(path, "version"); err == nil {
		if f := strings.Fields(firstLine(out)); len(f) >= 3 {
			detail = f[2] + " (" + path + ")"
		}
	}
	c.add("toolchain", "Go", OK, detail+"; "+why, "")
}

func (c *checker) shell() {
	path, source, err := c.p.FindShell()
	if err != nil {
		c.add("shell", "krate-shell", Fail, err.Error(), "")
		return
	}
	if path == "" {
		c.add("shell", "krate-shell", Fail, "not found",
			"the @krate/core-<platform> package ships it next to krate: reinstall @krate/core; or pass --shell <path> / set KRATE_SHELL")
		c.add("shell", "webview", Warn, "not probed: the webview check runs through krate-shell --doctor", "")
		return
	}
	c.add("shell", "krate-shell", OK, path+" ("+source+")", "")
	info, err := c.p.ShellDoctor(path)
	if err != nil {
		c.add("shell", "webview", Fail, "krate-shell --doctor failed: "+err.Error(), "the shell binary may be for another OS or architecture; "+
			"use the one of the @krate/core-"+c.p.GOOS+"-"+c.p.GOARCH+" package")
		return
	}
	if info.OS != "" && (info.OS != c.p.GOOS || info.Arch != c.p.GOARCH) {
		c.add("shell", "krate-shell target", Warn, fmt.Sprintf("the shell is built for %s/%s, this machine is %s/%s", info.OS, info.Arch, c.p.GOOS, c.p.GOARCH), "")
	}
	wv := info.Webview
	name := wv.Name
	if name == "" {
		name = "webview"
	}
	detail := wv.Version
	if wv.MinVersion != "" {
		detail += " (needs " + wv.MinVersion + " or newer)"
	}
	if info.OSVersion != "" {
		detail = strings.TrimSpace(detail + "; " + c.osLabel() + " " + info.OSVersion)
	}
	switch {
	case wv.OK:
		c.add("webview", name, OK, detail, "")
		c.r.Items[len(c.r.Items)-1].Paths = wv.Libraries
	default:
		msg := wv.Error
		if msg == "" {
			msg = "not usable"
		}
		c.add("webview", name, Fail, msg, wv.Hint)
	}
	if info.Display.Available {
		c.add("webview", "display", OK, info.Display.Detail, "")
	} else {
		c.add("webview", "display", Warn, info.Display.Detail, "windows cannot open here; builds and --browser mode still work")
	}
}

func (c *checker) osLabel() string {
	switch c.p.GOOS {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	}
	return "OS"
}

func (c *checker) runtimes() {
	seen := map[string]bool{}
	var list []string
	add := func(rt, why string) {
		if rt == "" || rt == "quickjs" || seen[rt] {
			return
		}
		seen[rt] = true
		list = append(list, rt+"|"+why)
	}
	add(c.in.HostRuntime, "native.host.runtime")
	isl := append([]string(nil), c.in.Islands...)
	sort.Strings(isl)
	for _, rt := range isl {
		add(rt, "native.host.islands")
	}
	if c.in.HostRuntime == "quickjs" && len(list) == 0 {
		c.add("runtimes", "quickjs", OK, "built into the shell; no external runtime is needed", "")
	}
	for _, e := range list {
		rt, why, _ := strings.Cut(e, "|")
		switch rt {
		case "node", "bun", "deno":
			c.hostRuntime(rt, why)
		case "scriptc":
			c.scriptc(why)
		}
	}
}

func (c *checker) hostRuntime(rt, why string) {
	env := "KRATE_" + strings.ToUpper(rt)
	exe := c.p.Getenv(env)
	from := env
	if exe == "" {
		path, err := c.p.LookPath(rt)
		if err != nil {
			c.add("runtimes", rt, Fail, "not found on PATH (selected by "+why+")", "install "+rt+" or set "+env+" to its executable")
			return
		}
		exe, from = path, "PATH"
	}
	out, err := c.p.Output(exe, "--version")
	if err != nil {
		c.add("runtimes", rt, Fail, "cannot run "+exe+" --version: "+err.Error(), "check that the file is a "+rt+" executable")
		return
	}
	have := versionNumber(out)
	detail := have + " (" + exe + ", from " + from + ")"
	if want := strings.TrimPrefix(strings.TrimSpace(c.in.HostVersion), "v"); want != "" && rt == c.in.HostRuntime && why == "native.host.runtime" {
		if have != want && !strings.HasPrefix(have, want+".") {
			c.add("runtimes", rt, Fail, fmt.Sprintf("version %s, but native.host.version is %q", have, c.in.HostVersion),
				"install the pinned version, point "+env+" at it, or change native.host.version")
			return
		}
	}
	c.add("runtimes", rt, OK, detail, "")
}

func versionNumber(out string) string {
	s := firstLine(out)
	s = strings.TrimPrefix(s, "v")
	for _, f := range strings.Fields(s) {
		f = strings.TrimPrefix(f, "v")
		if len(f) > 0 && f[0] >= '0' && f[0] <= '9' {
			return f
		}
	}
	return s
}

func (c *checker) scriptc(why string) {
	path, err := c.p.LookPath("scriptc")
	if err != nil {
		c.add("runtimes", "scriptc", Fail, "not found on PATH (selected by "+why+")", "npm install -g scriptc (needs Node 24 or later)")
		return
	}
	c.add("runtimes", "scriptc", OK, path, "")
	if c.p.Getenv("SCRIPTC_LINKER") != "" {
		c.add("runtimes", "scriptc linker", OK, "SCRIPTC_LINKER="+c.p.Getenv("SCRIPTC_LINKER"), "")
		return
	}
	linker := "clang"
	if c.p.GOOS == "windows" {
		linker = "zig"
	}
	if lp, err := c.p.LookPath(linker); err != nil {
		c.add("runtimes", "scriptc linker", Fail, linker+" not found on PATH", "install "+linker+", or set SCRIPTC_LINKER")
	} else {
		c.add("runtimes", "scriptc linker", OK, lp, "")
	}
}

func (c *checker) packaging() {
	const g = "packaging"
	has := func(l []string, s string) bool {
		for _, x := range l {
			if x == s {
				return true
			}
		}
		return false
	}
	switch c.p.GOOS {
	case "linux":
		c.tool(g, "appimagetool", "appimagetool", has(c.in.LinuxFormats, "appimage"), "builds .AppImage (native.bundle.linux.formats)")
		c.tool(g, "dpkg-deb", "dpkg-deb", false, "optional: .deb files are written without it")
		c.tool(g, "rpm", "rpm", false, "optional: .rpm files are written without it")
		c.tool(g, "makensis", "makensis", false, "optional: builds Windows NSIS installers from Linux")
		c.tool(g, "osslsigncode", "osslsigncode", false, "optional: signs Windows packages off Windows")
	case "darwin":
		c.tool(g, "hdiutil", "hdiutil", true, "builds .dmg files")
		c.tool(g, "codesign", "codesign", false, "optional: signs the app (krate native sign)")
		c.tool(g, "xcrun", "xcrun", false, "optional: runs notarytool for notarization (krate native notarize)")
		c.tool(g, "makensis", "makensis", false, "optional: builds Windows NSIS installers")
		c.tool(g, "osslsigncode", "osslsigncode", false, "optional: signs Windows packages")
	case "windows":
		c.tool(g, "makensis", "makensis", c.in.WindowsInstaller == "nsis", "builds the NSIS installer (native.bundle.windows.installer)")
		c.tool(g, "signtool", "signtool", false, "optional: signs .exe, .msi and .msix (krate native sign)")
		c.tool(g, "osslsigncode", "osslsigncode", false, "optional: alternative to signtool")
	}
}

// Format writes the report as text.
func Format(w io.Writer, r Report) {
	fmt.Fprintf(w, "Krate Native doctor (%s/%s)\n", r.OS, r.Arch)
	last := ""
	for _, it := range r.Items {
		if it.Group != last {
			last = it.Group
			fmt.Fprintf(w, "\n%s\n", titleOf(it.Group))
		}
		fmt.Fprintf(w, "  %-8s %-16s %s\n", label(it.Status), it.Name, it.Detail)
		for _, p := range it.Paths {
			fmt.Fprintf(w, "  %-8s %-16s %s\n", "", "", p)
		}
		if it.Hint != "" && (it.Status == Fail || it.Status == Warn) {
			fmt.Fprintf(w, "  %-8s %-16s hint: %s\n", "", "", it.Hint)
		}
	}
	if f := r.Failed(); len(f) > 0 {
		names := make([]string, len(f))
		for i, it := range f {
			names[i] = it.Name
		}
		fmt.Fprintf(w, "\n%d required item(s) missing: %s\n", len(f), strings.Join(names, ", "))
	} else {
		fmt.Fprintf(w, "\nEverything this project needs is in place.\n")
	}
}

func titleOf(g string) string {
	switch g {
	case "toolchain":
		return "Toolchain"
	case "shell":
		return "Shell"
	case "webview":
		return "Webview"
	case "runtimes":
		return "Host runtime"
	case "packaging":
		return "Packaging and signing"
	}
	return g
}

func label(s Status) string {
	switch s {
	case OK:
		return "ok"
	case Fail:
		return "MISSING"
	case Warn:
		return "warn"
	}
	return "-"
}
