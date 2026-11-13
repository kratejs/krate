package runtimes

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// ScriptcFailOn are the levels `scriptc coverage --fail-on` accepts.
var ScriptcFailOn = []string{"blockers", "divergences", "dynamic", "dynamic-slots", "type-claims"}

// ScriptcTriple is the SCRIPTC_TARGET value for a cross build. scriptc links
// macOS executables only on a Mac, so darwin has no cross triple.
func ScriptcTriple(t Target) (string, error) {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[t.Arch]
	switch {
	case arch == "":
	case t.OS == "linux":
		return arch + "-linux-gnu", nil
	case t.OS == "windows" && t.Arch == "amd64":
		return "x86_64-windows-gnu", nil
	}
	return "", unavailable(Scriptc, "scriptc cannot build for "+t.String()+" from "+HostTarget().String(),
		"build macOS executables on a Mac; Linux and Windows x64 targets need zig and the target's @scriptc/runtime pack")
}

// ScriptcToolchain checks that scriptc and the linker it needs for an
// executable for target are installed (KR7236). The linker is clang on macOS
// and glibc Linux and zig on Windows and for any cross build; SCRIPTC_LINKER
// replaces the check.
func ScriptcToolchain(target Target) error {
	if _, err := lookPath("scriptc"); err != nil {
		return diag.New("krate.config", 1, 1, "native.host.runtime scriptc: scriptc was not found on PATH",
			"install it with npm install -g scriptc (needs Node 24 or later)", "").WithCode(diag.CodeScriptcToolchain)
	}
	if envGet("SCRIPTC_LINKER") != "" {
		return nil
	}
	linker := "clang"
	if !target.IsHost() || target.OS == "windows" {
		linker = "zig"
	}
	if _, err := lookPath(linker); err != nil {
		why := "scriptc links " + target.String() + " executables with " + linker
		hint := "install " + linker
		if linker == "clang" {
			hint += " (macOS: xcode-select --install; Linux: your package manager), or set SCRIPTC_LINKER"
		} else {
			hint += " from ziglang.org, or set SCRIPTC_LINKER"
		}
		return diag.New("krate.config", 1, 1, "native.host.runtime scriptc: "+linker+" was not found on PATH; "+why, hint, "").WithCode(diag.CodeScriptcToolchain)
	}
	return nil
}

// ScriptcBuildArgs are the arguments and extra environment of `scriptc build`
// for a release executable.
func ScriptcBuildArgs(target Target, entry, exe string) (args, env []string, err error) {
	args = []string{"build", entry, "-o", exe, "--strip", "--optimization", "release"}
	if !target.IsHost() {
		t, err := ScriptcTriple(target)
		if err != nil {
			return nil, nil, err
		}
		env = []string{"SCRIPTC_TARGET=" + t}
	}
	return args, env, nil
}

// ScriptcCoverageArgs are the arguments of `scriptc coverage` for entry. The
// JSON diagnostics document replaces the human report; the exit status is 1
// when the --fail-on level is hit.
func ScriptcCoverageArgs(entry, failOn string) []string {
	if failOn == "" {
		failOn = "blockers"
	}
	return []string{"coverage", entry, "--print=diagnostics", "--fail-on=" + failOn}
}

func buildScriptc(target Target, in ReleaseInput, exe string) error {
	if err := ScriptcToolchain(target); err != nil {
		return err
	}
	args, env, err := ScriptcBuildArgs(target, in.Entry, exe)
	if err != nil {
		return err
	}
	if out, err := run(filepath.Dir(in.Entry), env, "scriptc", args...); err != nil {
		// A build that scriptc rejects prints its diagnostics; keep them whole.
		if ds, perr := ScriptcDiagnostics(out, Origin{}); perr == nil && len(ds) > 0 {
			return &ScriptcError{Diagnostics: ds}
		}
		return releaseFailed("scriptc build", out, err)
	}
	return nil
}

// ScriptcError is a release build scriptc rejected. There is no fallback to
// an interpreter: the build fails with these diagnostics.
type ScriptcError struct{ Diagnostics []diag.Diagnostic }

func (e *ScriptcError) Error() string {
	var parts []string
	for _, d := range e.Diagnostics {
		parts = append(parts, d.Error())
	}
	return strings.Join(parts, "\n")
}

// ScriptcReport is the part of scriptc's `--print=diagnostics` document
// (schema "scriptc-diagnostics", version 1) that Krate reads.
type ScriptcReport struct {
	Schema        string           `json:"schema"`
	SchemaVersion int              `json:"schemaVersion"`
	Command       string           `json:"command"`
	Entry         string           `json:"entry"`
	Success       bool             `json:"success"`
	Phase         string           `json:"phase"`
	Diagnostics   []ScriptcMessage `json:"diagnostics"`
}

// ScriptcMessage is one entry of the document's "diagnostics" array.
type ScriptcMessage struct {
	Code     string `json:"code"`
	Category string `json:"category"`
	Severity string `json:"severity"`
	Scope    string `json:"scope"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Message  string `json:"message"`
	Hint     string `json:"hint"`
}

// ParseScriptcReport decodes scriptc's JSON diagnostics document.
func ParseScriptcReport(b []byte) (*ScriptcReport, error) {
	var r ScriptcReport
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("scriptc: reading its diagnostics: %w", err)
	}
	if r.Schema != "scriptc-diagnostics" {
		return nil, fmt.Errorf("scriptc: unexpected output (schema %q)", r.Schema)
	}
	if r.SchemaVersion != 1 {
		return nil, fmt.Errorf("scriptc: diagnostics schema version %d is newer than this Krate understands (1)", r.SchemaVersion)
	}
	return &r, nil
}

// Origin maps a position in a generated bundle back to the project source.
type Origin struct {
	// Map is the bundle's source map (JSON); with it, positions in Bundle map
	// to the sources the map lists. Without it positions are kept.
	Map []byte
	// Bundle is the absolute path of the bundle scriptc was given.
	Bundle string
	// Root is the project root; reported files are relative to it.
	Root string
	// Remap, when set, adjusts a (source, line) pair, for example to turn a
	// line of a generated inline module into the line of the UI file.
	Remap func(source string, line int) (string, int)
	// RuntimeSource reports whether a source file is the Krate host child
	// runtime and not the app's code.
	RuntimeSource func(source string) bool
}

// ScriptcDiagnostics converts the diagnostics of a scriptc run into Krate
// diagnostics at the original file:line. Only errors scriptc counts against the
// build are returned: scope "reached" or "preflight" and severity error, and
// warnings (divergences and the like) as Krate warnings. A blocker inside the
// host child runtime gets KR7237 instead of KR7235.
func ScriptcDiagnostics(raw []byte, o Origin) ([]diag.Diagnostic, error) {
	rep, err := ParseScriptcReport(raw)
	if err != nil {
		return nil, err
	}
	var sm *SourceMap
	if len(o.Map) > 0 {
		if sm, err = ParseSourceMap(o.Map); err != nil {
			sm = nil
		}
	}
	var out []diag.Diagnostic
	for _, m := range rep.Diagnostics {
		if m.Scope != "reached" && m.Scope != "preflight" && m.Scope != "" {
			continue
		}
		file, line, col := m.File, m.Line, m.Column
		if sm != nil && (o.Bundle == "" || sameFile(m.File, o.Bundle)) {
			if src, l, c, ok := sm.Original(m.Line, m.Column); ok {
				file, line, col = src, l, c
				if o.Remap != nil {
					file, line = o.Remap(file, line)
				}
			}
		}
		code := diag.CodeScriptcUnsupported
		if o.RuntimeSource != nil && o.RuntimeSource(file) {
			code = diag.CodeScriptcRuntime
		}
		rel := file
		if o.Root != "" && filepath.IsAbs(file) {
			if r, err := filepath.Rel(o.Root, file); err == nil && !strings.HasPrefix(r, "..") {
				rel = r
			}
		}
		rel = filepath.ToSlash(rel)
		hint := m.Hint
		if hint == "" {
			hint = "see https://scriptc.dev/docs/limitations; or run this code on QuickJS or Node"
		}
		d := diag.New(rel, max(line, 1), max(col, 1), "scriptc "+m.Code+": "+m.Message, hint, sourceLine(o.Root, rel, line)).WithCode(code)
		if m.Severity == "warning" {
			d.Severity = diag.SeverityWarning
		}
		out = append(out, d)
	}
	return out, nil
}

func sameFile(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func sourceLine(root, rel string, line int) string {
	if root == "" || line < 1 {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	if line > len(lines) {
		return ""
	}
	return strings.TrimRight(lines[line-1], "\r")
}

// ScriptcCoverage runs `scriptc coverage` on entry and returns the converted
// diagnostics. A coverage run that hits the fail-on level exits 1 with the
// document on stdout; any other failure is an error. dir is the working
// directory (the project root, so node_modules resolve).
func ScriptcCoverage(dir, entry, failOn string, o Origin) ([]diag.Diagnostic, error) {
	if err := ScriptcToolchainForCoverage(); err != nil {
		return nil, err
	}
	out, runErr := run(dir, nil, "scriptc", ScriptcCoverageArgs(entry, failOn)...)
	ds, err := ScriptcDiagnostics(jsonPart(out), o)
	if err != nil {
		if runErr != nil {
			return nil, releaseFailed("scriptc coverage", out, runErr)
		}
		return nil, err
	}
	return ds, nil
}

// ScriptcToolchainForCoverage checks only that scriptc is installed: coverage
// analyses the program and needs no linker.
func ScriptcToolchainForCoverage() error {
	if _, err := lookPath("scriptc"); err != nil {
		return diag.New("krate.config", 1, 1, "native.host.runtime scriptc: scriptc was not found on PATH",
			"install it with npm install -g scriptc (needs Node 24 or later)", "").WithCode(diag.CodeScriptcToolchain)
	}
	return nil
}

// jsonPart returns the JSON object in out; scriptc may print progress lines
// around the document on stderr, which run merges into the output.
func jsonPart(out []byte) []byte {
	i := strings.Index(string(out), `{"schema"`)
	if i < 0 {
		i = strings.Index(string(out), "{")
	}
	if i < 0 {
		return out
	}
	j := strings.LastIndex(string(out), "}")
	if j < i {
		return out[i:]
	}
	return out[i : j+1]
}
