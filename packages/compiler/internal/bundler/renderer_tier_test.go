package bundler

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

func TestRendererTierConflicts(t *testing.T) {
	const canvas = "export default function Canvas() { return <div/>; }\n"
	cases := []struct {
		name, path, src string
		server          []string
		want            string
	}{
		{name: "decorator alone", path: "/p/Canvas.tsx", src: "@renderer\n" + canvas},
		{name: "with client", path: "/p/Canvas.tsx", src: "@renderer\n@client\n" + canvas},
		{name: "use renderer directive", path: "/p/Canvas.tsx", src: "export function Canvas() { \"use renderer\"; return <div/>; }\n"},
		{name: "with server decorator", path: "/p/Canvas.tsx", src: "@renderer\n@server\n" + canvas, want: "also marked @server"},
		{name: "directive with static directive", path: "/p/Canvas.tsx", src: "export function Canvas() { \"@static\"; \"use renderer\"; return <div/>; }\n", want: "also marked @static"},
		{name: "server comment", path: "/p/Canvas.tsx", src: "// @server\n@renderer\n" + canvas, want: "`// @server` comment"},
		{name: "runtime file name", path: "/p/Canvas.runtime.tsx", src: "@renderer\n" + canvas, want: "`.runtime` file name"},
		{name: "config list", path: "/p/Canvas.tsx", src: "@renderer\n" + canvas, server: []string{"Canvas"}, want: "`serverComponents`"},
		{name: "async directive form", path: "/p/Canvas.tsx", src: "export const Canvas = async () => { \"@renderer\"; return <div/>; };\n", want: "cannot be async"},
		{name: "client override in a server file", path: "/p/Canvas.tsx", src: "// @server\n@renderer\n@client\nexport function Canvas() { return <div/>; }\n" + "export default function P() { return <p/>; }\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := conflictOf(t, c.path, c.src, c.server, nil, nil, nil)
			if c.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want an error mentioning %q, got %v", c.want, err)
			}
			if d, ok := err.(diag.Diagnostic); !ok || d.Code != diag.CodeRendererTier {
				t.Errorf("want a KR7117 diagnostic, got %+v", err)
			}
		})
	}
}

// A malformed krate.vendor list fails the import that reaches the package.
func TestVendorFieldMalformed(t *testing.T) {
	resolver.ResetCaches()
	t.Cleanup(resolver.ResetCaches)
	dir := t.TempDir()
	pkg := filepath.Join(dir, "node_modules", "@krate", "bad")
	for path, src := range map[string]string{
		filepath.Join(pkg, "package.json"): `{"name":"@krate/bad","krate":{"vendor":"src/engine/"},"exports":{".":"./index.ts"}}`,
		filepath.Join(pkg, "index.ts"):     `export const x = 1;`,
		filepath.Join(dir, "page.tsx"):     "import { x } from '@krate/bad';\nexport default function P() { return <p>{x}</p>; }\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	_, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
	var d diag.Diagnostic
	if !errors.As(err, &d) || d.Code != diag.CodeVendorField {
		t.Fatalf("want a KR7127 diagnostic, got %v", err)
	}
	if !strings.Contains(d.Message, "krate.vendor") || d.Line != 1 {
		t.Errorf("diagnostic should name krate.vendor at the import: %+v", d)
	}
}
