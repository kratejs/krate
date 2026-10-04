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
	if !strings.Contains(scoped, "composes: "+mapping["base"]) {
		t.Errorf("composes local name should be scoped:\n%s", scoped)
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
