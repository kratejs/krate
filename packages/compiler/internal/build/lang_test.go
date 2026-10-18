package build

import "testing"

func TestDetectPageLang(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{`export const lang = "fr"; export default function P(){ return <div/>; }`, "fr"},
		{`export const config = { lang: "de" }; export default function P(){ return <div/>; }`, "de"},
		{`export default function P(){ return <div/>; }`, ""},
	}
	for _, c := range cases {
		if got := detectPageLang(parseInlineSource(c.src)); got != c.want {
			t.Errorf("detectPageLang(%q) = %q, want %q", c.src, got, c.want)
		}
	}
}
