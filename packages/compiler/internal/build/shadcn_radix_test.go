package build

import (
	"regexp"
	"strings"
	"testing"
)

// shadcnFakeRadix writes a stand-in for the radix-ui umbrella package: the
// umbrella re-exports each primitive as `import * as X; export { X }`, and
// each primitive names its root component after the primitive (`Switch`,
// exported again as `Root`). That is the shape that collides with a local
// wrapper called `Switch`.
func shadcnFakeRadix(t *testing.T, root string) {
	t.Helper()
	pkg := func(name, index string) {
		writeProjectFile(t, root, "node_modules/"+name+"/package.json",
			`{"name":"`+name+`","version":"1.0.0","main":"index.js","peerDependencies":{"react":"*"}}`)
		writeProjectFile(t, root, "node_modules/"+name+"/index.js", index)
	}
	primitive := func(name, tag string) {
		pkg("@fake/react-"+strings.ToLower(name), `
			import { jsx } from 'react/jsx-runtime';
			function `+name+`(props) { return jsx('`+tag+`', { 'data-primitive': '`+name+`', ...props }); }
			function `+name+`Part(props) { return jsx('i', { 'data-part': '`+name+`', ...props }); }
			export { `+name+`, `+name+` as Root, `+name+`Part as Part };
		`)
	}
	primitive("Switch", "button")
	primitive("Checkbox", "button")
	primitive("Dialog", "section")
	primitive("Tabs", "div")
	pkg("fake-radix", `
		import * as Switch from '@fake/react-switch';
		import * as Checkbox from '@fake/react-checkbox';
		import * as Dialog from '@fake/react-dialog';
		import * as Tabs from '@fake/react-tabs';
		export { Switch, Checkbox, Dialog, Tabs };
	`)
}

// shadcnWrapper is a component file as `shadcn add` writes it:
// `import { X as XPrimitive } from "radix-ui"` and a local function X.
func shadcnWrapper(name string, namespaceForm bool) string {
	imp := `import { ` + name + ` as ` + name + `Primitive } from "fake-radix";`
	if namespaceForm {
		imp = `import * as ` + name + `Primitive from "@fake/react-` + strings.ToLower(name) + `";`
	}
	return `import * as React from "react";
` + imp + `

function ` + name + `({ ...props }) {
  return <` + name + `Primitive.Root data-slot="` + strings.ToLower(name) + `" {...props} />;
}

function ` + name + `Part({ ...props }) {
  return <` + name + `Primitive.Part data-slot="part" {...props} />;
}

export { ` + name + `, ` + name + `Part };
`
}

// Two shadcn components on one page must both build. The alias of a member of
// the umbrella package must never be read as the local wrapper of the same
// name (`Switch as SwitchPrimitive` is not `function Switch`).
func TestAliasedExternalImportIsNotALocalOfTheSameName(t *testing.T) {
	cases := map[string]struct {
		comps     []string
		namespace bool
	}{
		"switch and checkbox":             {[]string{"Switch", "Checkbox"}, false},
		"dialog and tabs":                 {[]string{"Dialog", "Tabs"}, false},
		"all four":                        {[]string{"Dialog", "Tabs", "Switch", "Checkbox"}, false},
		"one alone":                       {[]string{"Dialog"}, false},
		"namespace import of the package": {[]string{"Dialog", "Tabs"}, true},
		"namespace import, all four":      {[]string{"Dialog", "Tabs", "Switch", "Checkbox"}, true},
	}
	for name, tc := range cases {
		comps := tc.comps
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			cfg := setupIslandProject(t, root, true)
			shadcnFakeRadix(t, root)
			var imports, body strings.Builder
			for _, c := range comps {
				writeProjectFile(t, root, "src/components/ui/"+strings.ToLower(c)+".tsx", shadcnWrapper(c, tc.namespace))
				imports.WriteString("import { " + c + ", " + c + "Part } from '../components/ui/" + strings.ToLower(c) + "';\n")
				body.WriteString("<" + c + " id=\"" + strings.ToLower(c) + "\"><" + c + "Part /></" + c + ">")
			}
			writeProjectFile(t, root, "src/pages/index.tsx",
				imports.String()+"export default function Page() { return <main>"+body.String()+"</main>; }\n")
			out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
			if err != nil {
				t.Fatalf("BuildAll: %v\n%s", err, out)
			}
			html := readBuilt(t, cfg.OutDir+"/index.html")
			for _, c := range comps {
				if !strings.Contains(html, `data-primitive="`+c+`"`) {
					t.Errorf("%s was not rendered (server markup missing):\n%s\nlog:\n%s", c, html, out)
				}
			}
			if regexp.MustCompile(`recursive component|KR8321|KR8008`).MatchString(out) {
				t.Errorf("unexpected diagnostics:\n%s", out)
			}
		})
	}
}

// `import { Slot } from "radix-ui"` (the umbrella package current shadcn
// components use) is Krate's native Slot, so asChild works under any local
// name, as it already does for @radix-ui/react-slot.
func TestRadixUmbrellaSlotAsChild(t *testing.T) {
	for _, local := range []string{"Slot", "SlotPrimitive"} {
		t.Run(local, func(t *testing.T) {
			html, logged := buildPlain(t, map[string]string{
				"src/components/ui/button.tsx": `import * as React from "react";
import { Slot as ` + local + ` } from "radix-ui";
function Button({ asChild = false, ...props }) {
  const Comp = asChild ? ` + local + `.Root : "button";
  return <Comp data-slot="button" {...props} />;
}
export { Button };
`,
				"src/pages/index.tsx": "import { Button } from '../components/ui/button';\nexport default function P() { return <main><Button asChild><a href=\"/x\">go</a></Button><Button>plain</Button></main>; }\n",
			})
			if !strings.Contains(html, `<main><a href=/x`) || !strings.Contains(html, `go</a><button data-slot=button>plain</button>`) {
				t.Errorf("asChild did not merge onto the child:\n%s\nlog:\n%s", rootOf(t, html), logged)
			}
			if strings.Contains(logged, "KR8320") {
				t.Errorf("tag reported as unresolvable:\n%s", logged)
			}
		})
	}
}
