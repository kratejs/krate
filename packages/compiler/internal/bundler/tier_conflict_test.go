package bundler

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

func conflictOf(t *testing.T, path, src string, server, runtime, serverDirs, runtimeDirs []string) error {
	t.Helper()
	prog, errs := tsstrip.Parse(path, src, tsstrip.Options{})
	if len(errs) > 0 {
		t.Fatalf("parse: %v", errs)
	}
	return checkTierConflicts(prog, path, src, server, runtime, serverDirs, runtimeDirs)
}

func TestTierConflictRule(t *testing.T) {
	const fn = "export default function C() { return <p/>; }\n"
	cases := []struct {
		name, path, src string
		server, runtime []string
		sdirs, rdirs    []string
		want            string // substring of the error, "" for none
	}{
		{name: "no tier", path: "/p/C.tsx", src: fn},
		{name: "comment only", path: "/p/C.tsx", src: "// @server\n" + fn},
		{name: "comment and decorator agree", path: "/p/C.tsx", src: "// @server\n@server\n" + fn},
		{name: "comment and decorator differ", path: "/p/C.tsx", src: "// @runtime\n@server\n" + fn, want: "`// @runtime` comment"},
		{name: "file name and decorator differ", path: "/p/C.server.tsx", src: "@runtime\n" + fn, want: "`.server` file name"},
		{name: "file name and decorator agree", path: "/p/C.server.tsx", src: "@server\n" + fn},
		{name: "list and decorator differ", path: "/p/C.tsx", src: "@runtime\n" + fn, server: []string{"C"}, want: "`serverComponents`"},
		{name: "dir and decorator differ", path: "/proj/src/ui/C.tsx", src: "@server\n" + fn, rdirs: []string{"src/ui"}, want: "`runtimeDirs`"},
		{name: "two file-wide sources differ", path: "/p/C.runtime.tsx", src: "// @server\n" + fn, want: "conflicting tiers"},
		{name: "two comments differ", path: "/p/C.tsx", src: "// @server\n// @runtime\n" + fn, want: "conflicting tiers"},
		{name: "both lists", path: "/p/C.tsx", src: fn, server: []string{"C"}, runtime: []string{"C"}, want: "conflicting tiers"},
		{name: "body directive and decorator", path: "/p/C.tsx", src: "@static\nexport default function C() { \"@server\"; return <p/>; }\n", want: "conflicting tiers on C"},
		{name: "body directive on default vs comment", path: "/p/C.tsx", src: "// @server\nexport default function C() { \"@runtime\"; return <p/>; }\n", want: "`// @server` comment"},
		{name: "client decorator on the default export of a server file", path: "/p/C.tsx", src: "// @server\n@client\n" + fn, want: "@client decorator"},
		{name: "client decorator on another component is an override", path: "/p/C.tsx", src: "// @server\n@client\nfunction B() { return <b/>; }\n" + fn},
		{name: "client decorator on a named export is an override", path: "/p/C.server.tsx", src: "@client\nexport function B() { return <b/>; }\nexport default function C() { return <p/>; }\n"},
		{name: "decorator on a helper differs from the file name", path: "/p/C.server.tsx", src: "@static\nfunction B() { return <b/>; }\n" + fn},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := conflictOf(t, c.path, c.src, c.server, c.runtime, c.sdirs, c.rdirs)
			if c.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want an error mentioning %q, got %v", c.want, err)
			}
			d, ok := err.(diag.Diagnostic)
			if !ok || d.Code != diag.CodeTierDecorator {
				t.Errorf("want a KR7115 diagnostic, got %T %+v", err, err)
			}
		})
	}
}
