package htmlspec

import (
	"reflect"
	"testing"
)

func TestAttr(t *testing.T) {
	cases := []struct {
		tag, name string
		v         Value
		want      string
	}{
		{"input", "disabled", BoolVal(true), " disabled"},
		{"input", "disabled", BoolVal(false), ""},
		{"input", "disabled", Value{Kind: Bare}, " disabled"},
		{"input", "disabled", StrVal("false"), " disabled"},
		{"input", "disabled", LooseVal("false"), ""},
		{"input", "disabled", LooseVal("0"), ""},
		{"input", "readOnly", BoolVal(true), " readonly"},
		{"input", "defaultChecked", BoolVal(true), " checked"},
		{"input", "defaultValue", StrVal("x"), ` value="x"`},
		{"video", "muted", BoolVal(true), " muted"},
		{"div", "hidden", StrVal("until-found"), ` hidden="until-found"`},
		{"div", "hidden", BoolVal(true), " hidden"},
		{"p", "translate", StrVal("no"), ` translate="no"`},
		{"p", "translate", LooseVal("yes"), ` translate="yes"`},
		{"div", "draggable", BoolVal(false), ` draggable="false"`},
		{"div", "spellCheck", BoolVal(true), ` spellcheck="true"`},
		{"div", "contentEditable", Value{Kind: Bare}, ` contenteditable="true"`},
		{"b", "aria-hidden", BoolVal(true), ` aria-hidden="true"`},
		{"b", "data-x", BoolVal(false), ` data-x="false"`},
		{"b", "data-x", Value{Kind: Bare}, " data-x"},
		{"div", "title", BoolVal(true), ""},
		{"div", "title", Value{Kind: Null}, ""},
		{"div", "title", LooseVal("undefined"), ""},
		{"div", "title", StrVal("null"), ` title="null"`},
		{"div", "title", StrVal(`a"<&'`), ` title="a&quot;&lt;&amp;&#39;"`},
		{"div", "className", StrVal("c"), ` class="c"`},
		{"label", "htmlFor", StrVal("i"), ` for="i"`},
		{"div", "tabIndex", Value{Kind: Number, Str: "0"}, ` tabindex="0"`},
		{"use", "xlinkHref", StrVal("#a"), ` xlink:href="#a"`},
		{"svg", "xmlnsXlink", StrVal("u"), ` xmlns:xlink="u"`},
		{"text", "xmlLang", StrVal("en"), ` xml:lang="en"`},
		{"marker", "markerHeight", StrVal("3"), ` markerHeight="3"`},
		{"svg", "viewBox", StrVal("0 0 1 1"), ` viewBox="0 0 1 1"`},
		{"path", "strokeLinecap", StrVal("round"), ` stroke-linecap="round"`},
		{"img", "src", StrVal(""), ""},
		{"a", "href", StrVal(""), ` href=""`},
		{"link", "href", StrVal(""), ""},
		{"a", "href", StrVal("/ok"), ` href="/ok"`},
		{"a", "href", StrVal("javascript:alert(1)"), ` href="` + "javascript:throw new Error(&#39;React has blocked a javascript: URL as a security precaution.&#39;)" + `"`},
		{"form", "action", StrVal("\x01 JAVA\tSCRIPT:x"), ` action="` + "javascript:throw new Error(&#39;React has blocked a javascript: URL as a security precaution.&#39;)" + `"`},
		{"button", "formAction", StrVal("jav\nascript:x"), ` formaction="` + "javascript:throw new Error(&#39;React has blocked a javascript: URL as a security precaution.&#39;)" + `"`},
		{"use", "xlinkHref", StrVal("javascript:x"), ` xlink:href="` + "javascript:throw new Error(&#39;React has blocked a javascript: URL as a security precaution.&#39;)" + `"`},
		{"a", "href", StrVal("javascripts:x"), ` href="javascripts:x"`},
	}
	for _, c := range cases {
		if got := Attr(c.tag, c.name, c.v); got != c.want {
			t.Errorf("Attr(%s, %s, %+v) = %q, want %q", c.tag, c.name, c.v, got, c.want)
		}
	}
}

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

func TestStyle(t *testing.T) {
	got := Style([]StyleEntry{
		{"fontSize", Value{Kind: Number, Str: "12"}},
		{"marginTop", Value{Kind: Number, Str: "0"}},
		{"lineHeight", Value{Kind: Number, Str: "1.5"}},
		{"zIndex", LooseVal("3")},
		{"flexGrow", Value{Kind: Number, Str: "1"}},
		{"--gap", Value{Kind: Number, Str: "4"}},
		{"WebkitLineClamp", Value{Kind: Number, Str: "2"}},
		{"msTransition", StrVal("none")},
		{"MozAppearance", StrVal("none")},
		{"width", LooseVal("10")},
		{"height", StrVal("10")},
		{"color", Value{Kind: Null}},
		{"display", BoolVal(false)},
		{"top", StrVal("")},
		{"left", LooseVal("undefined")},
	})
	want := "font-size:12px;margin-top:0;line-height:1.5;z-index:3;flex-grow:1;--gap:4;-webkit-line-clamp:2;-ms-transition:none;-moz-appearance:none;width:10px;height:10"
	if got != want {
		t.Errorf("Style =\n %s\nwant\n %s", got, want)
	}
	if Style(nil) != "" {
		t.Errorf("empty style should be empty")
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

func TestVoidBooleanTables(t *testing.T) {
	for _, v := range []string{"br", "img", "input", "wbr", "source"} {
		if !IsVoid(v) {
			t.Errorf("%s should be void", v)
		}
	}
	if IsVoid("div") || IsVoid("template") {
		t.Errorf("div/template are not void")
	}
	if IsBoolean("translate") {
		t.Errorf("translate is enumerated, not boolean")
	}
	for _, b := range []string{"truespeed", "inert", "readOnly", "noModule", "playsInline"} {
		if !IsBoolean(b) {
			t.Errorf("%s should be boolean", b)
		}
	}
	for _, p := range []string{"value", "checked", "selected", "muted", "defaultValue"} {
		if !IsPropertyAttr(p) {
			t.Errorf("%s should be a property attr", p)
		}
	}
}

func TestRawText(t *testing.T) {
	if got := RawText("script", "a</ScRiPt>b</style>"); got != `a<\/ScRiPt>b</style>` {
		t.Errorf("script: %s", got)
	}
	if got := RawText("style", "a{}</style><b>"); got != `a{}<\/style><b>` {
		t.Errorf("style: %s", got)
	}
	if got := RawText("script", "x<y"); got != "x<y" {
		t.Errorf("unchanged: %s", got)
	}
}

func TestSelectValues(t *testing.T) {
	cases := []struct {
		v    Value
		want []string
	}{
		{StrVal("a"), []string{"a"}},
		{StrVal("[a]"), []string{"[a]"}},
		{LooseVal(`["a", "c"]`), []string{"a", "c"}},
		{LooseVal(`['a','b, c']`), []string{"a", "b, c"}},
		{LooseVal("a\x1fb"), []string{"a", "b"}},
		{Value{Kind: Null}, nil},
	}
	for _, c := range cases {
		if got := SelectValues(c.v); !reflect.DeepEqual(got, c.want) {
			t.Errorf("SelectValues(%+v) = %q, want %q", c.v, got, c.want)
		}
	}
}
