package plugins

import (
	"archive/zip"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

const goodManifest = `{
  "name": "demo-plugin",
  "version": "1.0.0",
  "krate": ">=0.5.0",
  "go": "example.com/demo",
  "permissions": [{"name": "read", "description": "read"}],
  "ops": [{"name": "info", "permission": "read"}, {"name": "free"}]
}`

func codes(ds []diag.Diagnostic) string {
	var out []string
	for _, d := range ds {
		out = append(out, string(d.Code))
	}
	return strings.Join(out, ",")
}

func TestParseManifest(t *testing.T) {
	m, ds := ParseManifest([]byte(goodManifest), "krate-plugin.json")
	if len(ds) > 0 {
		t.Fatalf("good manifest: %v", ds)
	}
	if m.Name != "demo-plugin" || len(m.Ops) != 2 || m.Ops[0].Permission != "read" {
		t.Errorf("manifest = %+v", m)
	}
	for op, want := range map[Op]string{{Name: "info"}: "Info", {Name: "cpuLoad", Go: "CPULoad"}: "CPULoad", {Name: "x"}: "X"} {
		if got := op.GoName(); got != want {
			t.Errorf("GoName(%+v) = %s, want %s", op, got, want)
		}
	}
}

func TestParseManifestErrors(t *testing.T) {
	cases := []struct {
		name, json, code, contains string
	}{
		{"not json", `{`, "KR7251", "krate-plugin.json"},
		{"unknown field", `{"name":"demo","go":"example.com/d","ops":[],"bogus":1}`, "KR7251", "bogus"},
		{"bad name", `{"name":"Demo_Plugin","go":"example.com/d","ops":[]}`, "KR7251", "not a valid plugin name"},
		{"short name", `{"name":"d","go":"example.com/d","ops":[]}`, "KR7251", "not a valid plugin name"},
		{"no go path", `{"name":"demo","ops":[]}`, "KR7251", "import path"},
		{"bad go path", `{"name":"demo","go":"has space","ops":[]}`, "KR7251", "import path"},
		{"bad krate", `{"name":"demo","go":"example.com/d","krate":"soon","ops":[]}`, "KR7251", "not a version"},
		{"dup op", `{"name":"demo","go":"example.com/d","ops":[{"name":"a"},{"name":"a"}]}`, "KR7252", "declared twice"},
		{"dup permission", `{"name":"demo","go":"example.com/d","permissions":[{"name":"a"},{"name":"a"}],"ops":[]}`, "KR7252", "declared twice"},
		{"capitalized op", `{"name":"demo","go":"example.com/d","ops":[{"name":"Info"}]}`, "KR7251", "lowerCamel"},
		{"hyphenated op", `{"name":"demo","go":"example.com/d","ops":[{"name":"cpu-load"}]}`, "KR7251", "lowerCamel"},
		{"hyphenated permission", `{"name":"demo","go":"example.com/d","permissions":[{"name":"read-all"}],"ops":[]}`, "KR7251", "lowerCamel"},
		{"bad go name", `{"name":"demo","go":"example.com/d","ops":[{"name":"cpuLoad","go":"cpuLoad"}]}`, "KR7251", "exported Go identifier"},
		{"two ops one go name", `{"name":"demo","go":"example.com/d","ops":[{"name":"info"},{"name":"other","go":"Info"}]}`, "KR7252", "both bind"},
		{"undeclared permission", `{"name":"demo","go":"example.com/d","ops":[{"name":"a","permission":"x"}]}`, "KR7251", "does not declare"},
		{"bad permission name", `{"name":"demo","go":"example.com/d","permissions":[{"name":"Read"}],"ops":[]}`, "KR7251", "lowerCamel"},
		{"absolute ts", `{"name":"demo","go":"example.com/d","ts":"/etc/x","ops":[]}`, "KR7251", "relative"},
	}
	for _, c := range cases {
		_, ds := ParseManifest([]byte(c.json), "krate-plugin.json")
		if len(ds) == 0 {
			t.Errorf("%s: accepted", c.name)
			continue
		}
		if codes(ds) != c.code && !strings.Contains(codes(ds), c.code) {
			t.Errorf("%s: codes %s, want %s", c.name, codes(ds), c.code)
		}
		var text strings.Builder
		for _, d := range ds {
			text.WriteString(d.Message + " " + d.Hint)
		}
		if !strings.Contains(text.String(), c.contains) {
			t.Errorf("%s: diagnostics %q lack %q", c.name, text.String(), c.contains)
		}
	}
}

func TestVersions(t *testing.T) {
	for _, s := range []string{"0.5.0", ">=0.5.0", "^1.2", "v1.2.3", "1.2.3-beta.1"} {
		if _, ok := ParseVersion(s); !ok {
			t.Errorf("ParseVersion(%q) failed", s)
		}
	}
	for _, s := range []string{"", "x", "1.2.3.4", "1.-2"} {
		if _, ok := ParseVersion(s); ok {
			t.Errorf("ParseVersion(%q) succeeded", s)
		}
	}
	a, _ := ParseVersion("0.5.0")
	b, _ := ParseVersion("0.10.0")
	if !a.Less(b) || b.Less(a) || a.Less(a) {
		t.Error("version order")
	}
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func manifestFor(name, goPath string) string {
	return `{"name":"` + name + `","go":"` + goPath + `","ops":[{"name":"run"}]}`
}

func TestResolveLocalAndNPM(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	// A plugin inside the app's module.
	write(t, root, "native-plugins/hello/krate-plugin.json", manifestFor("hello", "example.com/app/native-plugins/hello"))
	// A plugin with its own module.
	write(t, root, "native-plugins/own/go.mod", "module example.com/own\n\ngo 1.26\n")
	write(t, root, "native-plugins/own/krate-plugin.json", manifestFor("own", "example.com/own"))
	// An npm package that ships a plugin in a subdirectory.
	write(t, root, "node_modules/@acme/krate-sqlite/package.json", `{"name":"@acme/krate-sqlite","krate":{"plugin":"go/krate-plugin.json"}}`)
	write(t, root, "node_modules/@acme/krate-sqlite/go/go.mod", "module github.com/acme/krate-sqlite\n\ngo 1.26\n")
	write(t, root, "node_modules/@acme/krate-sqlite/go/krate-plugin.json", manifestFor("sqlite", "github.com/acme/krate-sqlite"))

	list, ds := Resolve(root, []string{"./native-plugins/hello", "./native-plugins/own", "@acme/krate-sqlite"}, "krate.config.ts", "0.5.0")
	if len(ds) > 0 {
		t.Fatalf("diagnostics: %v", ds)
	}
	if len(list) != 3 {
		t.Fatalf("resolved %d plugins", len(list))
	}
	hello, own, sqlite := list[0], list[1], list[2]
	if hello.ModuleDir != root || hello.ModulePath != "example.com/app" || hello.ImportPath != "example.com/app/native-plugins/hello" || !hello.Local {
		t.Errorf("hello = %+v", hello)
	}
	if own.ModuleDir != filepath.Join(root, "native-plugins", "own") || own.ImportPath != "example.com/own" || !own.Local {
		t.Errorf("own = %+v", own)
	}
	if sqlite.Kind != "npm" || sqlite.Local || sqlite.ImportPath != "github.com/acme/krate-sqlite" {
		t.Errorf("sqlite = %+v", sqlite)
	}
}

func TestResolveDiagnostics(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	write(t, root, "p/wrongpath/krate-plugin.json", manifestFor("wrongpath", "example.com/other/path"))
	write(t, root, "p/nomod/go.mod", "") // empty go.mod: no module line
	write(t, root, "p/nomod/krate-plugin.json", manifestFor("nomod", "example.com/nomod"))
	write(t, root, "p/new/krate-plugin.json", `{"name":"new","krate":"9.0.0","go":"example.com/app/p/new","ops":[]}`)
	write(t, root, "p/badname/krate-plugin.json", manifestFor("Bad", "example.com/app/p/badname"))
	write(t, root, "p/dup1/krate-plugin.json", manifestFor("same", "example.com/app/p/dup1"))
	write(t, root, "p/dup2/krate-plugin.json", manifestFor("same", "example.com/app/p/dup2"))
	write(t, root, "node_modules/emptypkg/package.json", `{"name":"emptypkg"}`)

	cases := []struct {
		spec, code, contains string
	}{
		{"./p/missing", "KR7250", "no krate-plugin.json"},
		{"./p/wrongpath", "KR7253", "example.com/app/p/wrongpath"},
		{"./p/nomod", "KR7253", "go.mod"},
		{"./p/new", "KR7254", "needs Krate 9.0.0 or newer"},
		{"./p/badname", "KR7251", "not a valid plugin name"},
		{"notinstalled", "KR7250", "not installed"},
		{"emptypkg", "KR7250", "no krate-plugin.json"},
	}
	for _, c := range cases {
		_, ds := Resolve(root, []string{c.spec}, "krate.config.ts", "0.5.0")
		if len(ds) == 0 || ds[0].Code != diag.Code(c.code) || !strings.Contains(ds[0].Message+ds[0].Hint, c.contains) {
			t.Errorf("%s: %v, want %s containing %q", c.spec, ds, c.code, c.contains)
		}
	}
	// A development build of krate skips the minimum version.
	if _, ds := Resolve(root, []string{"./p/new"}, "krate.config.ts", "dev"); len(ds) > 0 {
		t.Errorf("dev version: %v", ds)
	}
	// Two plugins with one name.
	_, ds := Resolve(root, []string{"./p/dup1", "./p/dup2"}, "krate.config.ts", "0.5.0")
	if len(ds) != 1 || ds[0].Code != diag.CodePluginDuplicate {
		t.Errorf("duplicate names: %v", ds)
	}
}

func TestDigestChangesWithSources(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	write(t, root, "p/krate-plugin.json", manifestFor("dg", "example.com/app/p"))
	write(t, root, "p/p.go", "package p\n")
	list, ds := Resolve(root, []string{"./p"}, "c", "dev")
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	a := list[0].Digest()
	write(t, root, "p/p.go", "package p\n\nfunc X() {}\n")
	if b := list[0].Digest(); a == b {
		t.Error("digest ignores Go source changes")
	}
	write(t, root, "p/p_test.go", "package p\n")
	c := list[0].Digest()
	write(t, root, "p/p_test.go", "package p\n// changed\n")
	if d := list[0].Digest(); c != d {
		t.Error("digest includes test files")
	}
}

func TestHasInit(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.go", "package a\n\nimport \"example.com/native/plugin\"\n\nfunc Init(c *plugin.Context) error { return nil }\n")
	if !hasInit(dir) {
		t.Error("Init with *plugin.Context not found")
	}
	other := t.TempDir()
	write(t, other, "a.go", "package a\n\nfunc Init() error { return nil }\n")
	write(t, other, "b.go", "package a\n\ntype T struct{}\nfunc (T) Init(c *T) error { return nil }\n")
	if hasInit(other) {
		t.Error("an Init with another signature counts")
	}
}

func TestScaffold(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "native-plugins", "my-tool")
	files, d := Scaffold(dir, ScaffoldOptions{Name: "my-tool", Module: "github.com/me/krate-my-tool", NativeReplace: "../../packages/native"})
	if d != nil {
		t.Fatal(d)
	}
	want := "krate-plugin.json go.mod mytool.go mytool_test.go README.md"
	if strings.Join(files, " ") != want {
		t.Errorf("files = %v", files)
	}
	m, ds := ParseManifest(readFile(t, filepath.Join(dir, "krate-plugin.json")), "krate-plugin.json")
	if len(ds) > 0 || m.Go != "github.com/me/krate-my-tool" || m.Name != "my-tool" {
		t.Errorf("scaffolded manifest: %+v %v", m, ds)
	}
	if gomod := string(readFile(t, filepath.Join(dir, "go.mod"))); !strings.Contains(gomod, "replace github.com/kratejs/krate/packages/native => ../../packages/native") {
		t.Errorf("go.mod:\n%s", gomod)
	}
	if src := string(readFile(t, filepath.Join(dir, "mytool.go"))); !strings.Contains(src, "package mytool") || !strings.Contains(src, "func Hello(") {
		t.Errorf("go source:\n%s", src)
	}
	// It does not overwrite, and it rejects bad names.
	if _, d := Scaffold(dir, ScaffoldOptions{Name: "my-tool"}); d == nil || !strings.Contains(d.Message, "already exists") {
		t.Errorf("second scaffold: %v", d)
	}
	if _, d := Scaffold(filepath.Join(t.TempDir(), "x"), ScaffoldOptions{Name: "Bad Name"}); d == nil {
		t.Error("a bad name was accepted")
	}
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeProxy serves one module version from a file:// GOPROXY directory.
func fakeProxy(t *testing.T, module, version string, files map[string]string) {
	t.Helper()
	proxy := t.TempDir()
	dir := filepath.Join(proxy, filepath.FromSlash(module), "@v")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, version+".info", `{"Version":"`+version+`","Time":"2026-01-01T00:00:00Z"}`)
	write(t, dir, version+".mod", files["go.mod"])
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	for name, body := range files {
		w, err := zw.Create(module + "@" + version + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, version+".zip"), zb.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPROXY", "file://"+filepath.ToSlash(proxy))
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOFLAGS", "-modcacherw")
	t.Setenv("GOMODCACHE", t.TempDir())
	t.Setenv("GOWORK", "off")
}

func TestResolveGoModule(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}
	fakeProxy(t, "example.com/krate-plugin-remote", "v1.2.0", map[string]string{
		"go.mod":            "module example.com/krate-plugin-remote\n\ngo 1.26\n",
		"krate-plugin.json": manifestFor("remote", "example.com/krate-plugin-remote"),
		"remote.go":         "package remote\n",
	})
	root := t.TempDir()
	list, ds := Resolve(root, []string{"go:example.com/krate-plugin-remote@v1.2.0"}, "krate.config.ts", "0.5.0")
	if len(ds) > 0 {
		t.Fatalf("diagnostics: %v", ds)
	}
	p := list[0]
	if p.Kind != "go" || p.Version != "v1.2.0" || p.ImportPath != "example.com/krate-plugin-remote" || p.Local || p.Manifest.Name != "remote" {
		t.Errorf("plugin = %+v", p)
	}
	// An unknown version and an unknown module are KR7253 with a hint.
	for _, spec := range []string{"go:example.com/krate-plugin-remote@v9.9.9", "go:example.com/nothing-here@v1.0.0"} {
		_, ds := Resolve(root, []string{spec}, "krate.config.ts", "0.5.0")
		if len(ds) != 1 || ds[0].Code != diag.CodePluginGoImport || !strings.Contains(ds[0].Message, "cannot resolve the Go module") || ds[0].Hint == "" {
			t.Errorf("%s: %v", spec, ds)
		}
	}
	// Without a version the app's go.mod has to require the module.
	_, ds = Resolve(root, []string{"go:example.com/krate-plugin-remote"}, "krate.config.ts", "0.5.0")
	if len(ds) != 1 || ds[0].Code != diag.CodePluginGoImport {
		t.Errorf("no version: %v", ds)
	}
}
