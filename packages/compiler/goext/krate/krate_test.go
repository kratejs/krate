package krate

import (
	"fmt"
	"strings"
	"testing"
)

func TestElEscapesText(t *testing.T) {
	got := string(Span(Class("badge"), Text(`<b>hi</b>`)).Render())
	want := `<span class="badge">&lt;b&gt;hi&lt;/b&gt;</span>`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestElRawIsVerbatim(t *testing.T) {
	got := string(Div(nil, Raw("<b>ok</b>")).Render())
	if got != "<div><b>ok</b></div>" {
		t.Fatalf("got %q", got)
	}
}

func TestAttrsRendering(t *testing.T) {
	got := string(Div(Attrs{
		"id":        "a&b",
		"hidden":    true,
		"checked":   false,
		"data-x":    "1",
		"tabindex":  3,
		"bad\"name": "v",
	}, Text("x")).Render())
	want := `<div data-x="1" hidden id="a&amp;b" tabindex="3">x</div>`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestClassAndStyleHelpers(t *testing.T) {
	got := string(Div(Class("a", "", "b"), nil).Render())
	if got != `<div class="a b"></div>` {
		t.Fatalf("class got %q", got)
	}
	got = string(Div(Style("color", "red", "margin", "0"), nil).Render())
	if got != `<div style="color:red;margin:0"></div>` {
		t.Fatalf("style got %q", got)
	}
}

func TestVoidElements(t *testing.T) {
	if got := string(Img(Attrs{"src": "/a.png", "alt": "a"}).Render()); got != `<img alt="a" src="/a.png">` {
		t.Fatalf("img got %q", got)
	}
	if got := string(Br(nil).Render()); got != "<br>" {
		t.Fatalf("br got %q", got)
	}
}

func TestInvalidTagRendersNothing(t *testing.T) {
	if got := string(El(`div onclick=alert(1)`, nil, Text("x")).Render()); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestFragJoinEach(t *testing.T) {
	got := string(Each([]int{1, 2, 3}, func(n int) Node { return Li(nil, Text(fmt.Sprint(n))) }).Render())
	if got != "<li>1</li><li>2</li><li>3</li>" {
		t.Fatalf("each got %q", got)
	}
	got = string(Join(Raw(", "), Text("a"), Text("b")).Render())
	if got != "a, b" {
		t.Fatalf("join got %q", got)
	}
	if string(Frag(Text("x"), Text("y")).Render()) != "xy" {
		t.Fatalf("frag wrong")
	}
}

func TestTemplateEscaping(t *testing.T) {
	tpl := MustTemplate(`<p class="{{.Class}}">{{.Msg}}</p>`)
	out, err := tpl.Render(map[string]string{"Class": "a&b", "Msg": "<b>x</b>"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `<p class="a&amp;b">&lt;b&gt;x&lt;/b&gt;</p>` {
		t.Fatalf("got %q", out)
	}
}

func TestSafeHTMLPassthrough(t *testing.T) {
	tpl := MustTemplate(`<div>{{.Inner}}</div>`)
	out, err := tpl.Render(map[string]any{"Inner": SafeHTML(Strong(nil, Text("hi")))})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "<div><strong>hi</strong></div>" {
		t.Fatalf("got %q", out)
	}
}

// TestNodeStringerContract locks the registry contract: the generated wrapper
// emits markup via fmt.Sprint, so a Node must stringify to its rendered HTML.
func TestNodeStringerContract(t *testing.T) {
	var n Node = Div(Class("c"), Text("x"))
	if fmt.Sprint(n) != string(n.Render()) {
		t.Fatalf("fmt.Sprint(node)=%q render=%q", fmt.Sprint(n), n.Render())
	}
	if !strings.Contains(fmt.Sprint(n), "<div") {
		t.Fatalf("unexpected %q", fmt.Sprint(n))
	}
}
