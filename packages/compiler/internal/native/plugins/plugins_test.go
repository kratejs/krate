package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func codes(ds []diag.Diagnostic) string {
	var out []string
	for _, d := range ds {
		out = append(out, string(d.Code))
	}
	return strings.Join(out, ",")
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
