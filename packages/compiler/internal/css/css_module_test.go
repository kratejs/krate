package css

import (
	"strings"
	"testing"
)

func TestCSSModuleScopesClasses(t *testing.T) {
	scoped, mapping, err := ProcessModule("/proj/a.module.css", ".foo { color: red; } .bar.foo { color: blue; }")
	if err != nil {
		t.Fatal(err)
	}
	foo := mapping["foo"]
	if foo == "" || !strings.HasPrefix(foo, "foo_") {
		t.Fatalf("foo mapping = %q", foo)
	}
	if !strings.Contains(scoped, "."+foo) {
		t.Errorf("scoped class missing:\n%s", scoped)
	}
	if strings.Contains(scoped, ".foo ") || strings.Contains(scoped, ".foo{") {
		t.Errorf("unscoped .foo leaked:\n%s", scoped)
	}
}

func TestCSSModuleGlobalIsUnscoped(t *testing.T) {
	scoped, mapping, err := ProcessModule("/proj/a.module.css", ":global(.btn) { color: red; }")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(scoped, ".btn {") {
		t.Errorf(":global inner should stay unscoped:\n%s", scoped)
	}
	if strings.Contains(scoped, ":global(") {
		t.Errorf(":global wrapper should be removed:\n%s", scoped)
	}
	if _, ok := mapping["btn"]; ok {
		t.Errorf("global class must not be scoped: %v", mapping)
	}
}

func TestCSSModuleDoesNotCorruptUrlsOrAttributeStrings(t *testing.T) {
	scoped, _, err := ProcessModule("/proj/a.module.css", `.a { background: url(./icon.svg); } .b[data-x=".c"] { color: red; }`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(scoped, "url(./icon.svg)") {
		t.Errorf("url() must be untouched:\n%s", scoped)
	}
	if !strings.Contains(scoped, `[data-x=".c"]`) {
		t.Errorf("attribute string must be untouched:\n%s", scoped)
	}
}

func TestCSSModuleComposes(t *testing.T) {
	scoped, mapping, err := ProcessModule("/proj/a.module.css", ".base { color: red; } .btn { composes: base; color: blue; }")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(scoped, "composes") {
		t.Errorf("composes declaration must be stripped:\n%s", scoped)
	}
	wantBtn := mapping["btn"]
	if !strings.HasPrefix(wantBtn, "btn_") || !strings.HasSuffix(wantBtn, " "+strings.Fields(mapping["base"])[0]) {
		t.Errorf("btn should list its own class then base, got %q", wantBtn)
	}
}

func TestCSSModuleKeyframesUntouched(t *testing.T) {
	src := "@keyframes spin { from { opacity: 0; } to { opacity: 1; } }"
	scoped, _, err := ProcessModule("/proj/a.module.css", src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(scoped, "@keyframes spin") || !strings.Contains(scoped, "from {") {
		t.Errorf("keyframes must be preserved:\n%s", scoped)
	}
}

func TestCSSModuleComposesForms(t *testing.T) {
	src := `.a { color: red; }
.b { background: blue; }
.c { composes: a b; margin: 0 }
.d { composes: c }
.e { composes: x y from global; }
.f { composes: a from "./other.module.css" }
.g:hover { composes: a; }
`
	res := ProcessModuleWith("/p/m.module.css", src, ModuleOptions{
		Composed: func(spec string) (map[string]string, error) {
			if spec != "./other.module.css" {
				t.Fatalf("spec %q", spec)
			}
			return map[string]string{"a": "a_zzz"}, nil
		},
	})
	if strings.Contains(res.CSS, "composes") {
		t.Errorf("composes left in CSS:\n%s", res.CSS)
	}
	m := res.Mapping
	if got := strings.Fields(m["c"]); len(got) != 3 || got[1] != m["a"] || got[2] != m["b"] {
		t.Errorf("c = %q", m["c"])
	}
	if got := strings.Fields(m["d"]); len(got) != 4 {
		t.Errorf("transitive composes: d = %q", m["d"])
	}
	if got := strings.Fields(m["e"]); len(got) != 3 || got[1] != "x" || got[2] != "y" {
		t.Errorf("global composes: e = %q", m["e"])
	}
	if got := strings.Fields(m["f"]); len(got) != 2 || got[1] != "a_zzz" {
		t.Errorf("from composes: f = %q", m["f"])
	}
	if len(res.ComposesFrom) != 1 || res.ComposesFrom[0] != "./other.module.css" {
		t.Errorf("ComposesFrom = %v", res.ComposesFrom)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "single class") {
		t.Errorf("warnings = %v", res.Warnings)
	}
}

func TestCSSModuleComposesUndefinedWarns(t *testing.T) {
	res := ProcessModuleWith("/p/m.module.css", ".a { composes: nope; }", ModuleOptions{})
	if len(res.Warnings) == 0 {
		t.Fatal("expected a warning for an undefined composes target")
	}
}

func TestCSSModuleKeyframesScoped(t *testing.T) {
	src := `@keyframes pop { from { opacity: 0 } to { opacity: 1 } }
@keyframes :global(shared) { from { opacity: 0 } }
.a { animation: pop 1s ease-in-out infinite, shared 2s; }
.b { animation-name: pop, other; -webkit-animation: 2s linear pop; }
.c { animation: :global(pop) 1s; }
@media (min-width: 1px) { .d { animation: 1s pop } }
.e { animation: var(--x) 1s; }
`
	res := ProcessModuleWith("/p/a.module.css", src, ModuleOptions{})
	pop := res.Mapping["pop"]
	if !strings.HasPrefix(pop, "pop_") {
		t.Fatalf("keyframes name not exposed: %v", res.Mapping)
	}
	for _, want := range []string{
		"@keyframes " + pop + " {",
		"@keyframes shared {",
		"animation: " + pop + " 1s ease-in-out infinite, shared 2s;",
		"animation-name: " + pop + ", other;",
		"-webkit-animation: 2s linear " + pop + ";",
		"animation: pop 1s;",
		"animation: 1s " + pop + " }",
		"animation: var(--x) 1s;",
	} {
		if !strings.Contains(res.CSS, want) {
			t.Errorf("missing %q in:\n%s", want, res.CSS)
		}
	}
	other := ProcessModuleWith("/p/b.module.css", "@keyframes pop { to { opacity: 0 } } .a { animation: pop 1s }", ModuleOptions{})
	if other.Mapping["pop"] == pop {
		t.Error("two modules must not share keyframes names")
	}
}

func TestCSSModuleBareGlobalAndLocal(t *testing.T) {
	res := ProcessModuleWith("/p/a.module.css", `:global .g1 { a: b }
.x :global .g2 .g3 { a: b }
:global .g4, .loc { a: b }
:global .g5 :local .l6 { a: b }
:local(.l7) :global(.g8) { a: b }
`, ModuleOptions{})
	m := res.Mapping
	for _, want := range []string{
		".g1 {",
		"." + m["x"] + " .g2 .g3 {",
		".g4, ." + m["loc"] + " {",
		".g5 ." + m["l6"] + " {",
		"." + m["l7"] + " .g8 {",
	} {
		if !strings.Contains(res.CSS, want) {
			t.Errorf("missing %q in:\n%s", want, res.CSS)
		}
	}
	if strings.Contains(res.CSS, ":global") || strings.Contains(res.CSS, ":local") {
		t.Errorf("mode keyword left behind:\n%s", res.CSS)
	}
	for _, g := range []string{"g1", "g2", "g3", "g4", "g5", "g8"} {
		if _, ok := m[g]; ok {
			t.Errorf("global class %s leaked into mapping", g)
		}
	}
}

func TestCSSModuleCamelCaseAliases(t *testing.T) {
	res := ProcessModuleWith("/p/a.module.css", ".card-title { a: b } .sub_item { a: b } .cardTitle2 { a: b }", ModuleOptions{})
	m := res.Mapping
	if m["cardTitle"] != m["card-title"] || m["subItem"] != m["sub_item"] || m["card-title"] == "" {
		t.Errorf("aliases missing: %v", m)
	}
}
