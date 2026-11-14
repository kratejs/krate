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
