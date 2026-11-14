package krate

import (
	"testing"
)

func TestElEscapesText(t *testing.T) {
	got := string(Span(Class("badge"), Text(`<b>hi</b>`)).Render())
	want := `<span class="badge">&lt;b&gt;hi&lt;/b&gt;</span>`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
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
