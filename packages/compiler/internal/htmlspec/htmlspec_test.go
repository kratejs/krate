package htmlspec

import (
	"testing"
)

func TestIsJavaScriptURL(t *testing.T) {
	yes := []string{"javascript:", "JavaScript:alert(1)", " \x00\x1fjavascript:x", "j\ta\nv\rascript:x"}
	no := []string{"", "http://x", "javascript", "java script:x", "/javascript:x", " javascript:x"}
	for _, u := range yes {
		if !IsJavaScriptURL(u) {
			t.Errorf("IsJavaScriptURL(%q) = false", u)
		}
	}
	for _, u := range no {
		if IsJavaScriptURL(u) {
			t.Errorf("IsJavaScriptURL(%q) = true", u)
		}
	}
}

func TestNameValidation(t *testing.T) {
	for _, ok := range []string{"div", "my-el", "svg:rect", "x.y", "h1"} {
		if !ValidTagName(ok) {
			t.Errorf("ValidTagName(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "1a", "-a", "a b", "a>", "a\"", "a/b", "a<"} {
		if ValidTagName(bad) {
			t.Errorf("ValidTagName(%q) = true", bad)
		}
	}
	for _, ok := range []string{"class", "data-x", "aria-label", "xlink:href", "_x", "x.y", "é"} {
		if !ValidAttrName(ok) {
			t.Errorf("ValidAttrName(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "1x", "-x", "a b", `a"`, "a>", "a=b", "a/b", "@click", "a'"} {
		if ValidAttrName(bad) {
			t.Errorf("ValidAttrName(%q) = true", bad)
		}
	}
}
