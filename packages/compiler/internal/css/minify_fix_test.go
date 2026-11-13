package css

import (
	"strings"
	"testing"
)

func TestMinifyExact(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		// 1/12: value transforms never touch selectors, preludes or names.
		{"hex in class selector", `.bg-\[\#ffffff\]{background-color:#ffffff}`, `.bg-\[\#ffffff\]{background-color:#fff}`},
		{"zero in class selector", `.top-\[0px\]{top:0px}`, `.top-\[0px\]{top:0}`},
		{"decimal in class selector", `.p-\[1\.0rem\]{padding:1.0rem}`, `.p-\[1\.0rem\]{padding:1.0rem}`},
		{"id selector", `#aabbcc{color:red}`, `#aabbcc{color:red}`},
		{"keyframes name", `@keyframes slide-0px{from{left:0px}to{left:10px}}`, `@keyframes slide-0px{from{left:0}to{left:10px}}`},
		{"media prelude", `@media (min-width:0px){.a{color:red}}`, `@media (min-width:0px){.a{color:red}}`},

		// 2: empty rules are removed brace-aware.
		{"empty in media", `@media screen{.a{}}.b{color:red}`, `.b{color:red}`},
		{"empty nested", `.a{color:red;&:hover{}}`, `.a{color:red}`},
		{"empty keyframe frame", `@keyframes k{0%{}100%{opacity:1}}`, `@keyframes k{100%{opacity:1}}`},
		{"empty rule between", `.a{color:red}.e{}.b{color:blue}`, `.a{color:red}.b{color:blue}`},
		{"empty layer kept", `@layer base{}`, `@layer base{}`},
		{"emptied layer kept", `@layer base{.a{}}`, `@layer base{}`},
		{"supports emptied", `@supports (display:grid){.a{ }}.b{color:red}`, `.b{color:red}`},

		// 3: zero units.
		{"calc zero px", `.a{width:calc(100% - 0px)}`, `.a{width:100%}`},
		{"calc zero px in min", `.a{width:min(calc(100% - 0px),5px)}`, `.a{width:min(100%,5px)}`},
		{"calc zero px two terms", `.a{width:calc(100% - 0px - 1px)}`, `.a{width:calc(100% - 0px - 1px)}`},
		{"max zero", `.a{width:max(0px,10px)}`, `.a{width:max(0px,10px)}`},
		{"clamp zero", `.a{width:clamp(0px,50%,100px)}`, `.a{width:clamp(0px,50%,100px)}`},
		{"flex 1 0px", `.a{flex:1 0px}`, `.a{flex:1 0px}`},
		{"flex 0px", `.a{flex:0px}`, `.a{flex:0px}`},
		{"flex-basis", `.a{flex-basis:0px}`, `.a{flex-basis:0px}`},
		{"grid 0fr", `.a{grid-template-rows:0fr}`, `.a{grid-template-rows:0fr}`},
		{"grid 1fr 0fr", `.a{grid-template-columns:1fr 0fr}`, `.a{grid-template-columns:1fr 0fr}`},
		{"decimal 10.0px", `.a{width:10.0px}`, `.a{width:10.0px}`},
		{"translate 1.0px", `.a{transform:translateX(1.0px)}`, `.a{transform:translateX(1.0px)}`},
		{"time and angle kept", `.a{transition:all 0s;transform:rotate(0deg);transition-delay:0ms}`, `.a{transition:all 0s;transform:rotate(0deg);transition-delay:0ms}`},
		{"custom property kept", `.a{--x:0px;width:0px}`, `.a{--x:0px;width:0}`},
		{"zero shorthand", `.a{margin:0px 0em 0rem 0px}`, `.a{margin:0 0 0 0}`},

		// 4: calc folding stays type-correct.
		{"calc mul one multi", `.a{width:calc(1 * 10px + 5px)}`, `.a{width:calc(1 * 10px + 5px)}`},
		{"calc add mul one", `.a{width:calc(10px + 5px * 1)}`, `.a{width:calc(10px + 5px * 1)}`},
		{"calc sub mul one", `.a{width:calc(100% - 1px * 1)}`, `.a{width:calc(100% - 1px * 1)}`},
		{"calc zero add chain", `.a{width:calc(0px + 1px - 2px)}`, `.a{width:calc(0px + 1px - 2px)}`},
		{"calc zero minus var", `.a{animation:a 1s calc(0 - var(--d))}`, `.a{animation:a 1s calc(0 - var(--d))}`},
		{"calc mul zero tail", `.a{width:calc(5px + 10px * 0)}`, `.a{width:calc(5px + 10px * 0)}`},
		{"calc mul zero head", `.a{width:calc(0 * 2px + 5px)}`, `.a{width:calc(0 * 2px + 5px)}`},
		{"calc time zero", `.a{transition-duration:calc(1s * 0)}`, `.a{transition-duration:0s}`},
		{"min calc zero", `.a{width:min(calc(1px * 0),10px)}`, `.a{width:min(0px,10px)}`},
		{"calc custom property", `.a{--x:calc(1px * 0)}`, `.a{--x:calc(1px * 0)}`},
		{"calc single op", `.a{width:calc(10px * 1)}`, `.a{width:10px}`},
		{"calc zero add", `.a{width:calc(0px + 10px)}`, `.a{width:10px}`},
		{"calc sub from zero", `.a{margin-left:calc(0px - 10px)}`, `.a{margin-left:-10px}`},
		{"calc mul zero to unit", `.a{margin-left:calc(10px * 0)}`, `.a{margin-left:0}`},
		{"calc nested", `.a{width:calc(100% - calc(1px * 1))}`, `.a{width:calc(100% - 1px)}`},
		{"webkit calc untouched", `.a{width:-webkit-calc(1px * 1)}`, `.a{width:-webkit-calc(1px * 1)}`},

		// 5: dedupe keeps the last occurrence at its own position.
		{"bg order", `.a{background-color:red;background:blue;background-color:green}`, `.a{background-color:red;background:blue;background-color:green}`},
		{"border order", `.a{border-color:red;border:1px solid;border-color:blue}`, `.a{border-color:red;border:1px solid;border-color:blue}`},
		{"margin order", `.a{margin-top:1px;margin:2px;margin-top:3px}`, `.a{margin-top:1px;margin:2px;margin-top:3px}`},
		{"unrelated between", `.a{padding:1px;color:red;padding:2px}`, `.a{color:red;padding:2px}`},

		// 6: intentional fallbacks survive.
		{"width fallback", `.a{width:100%;width:-moz-fit-content}`, `.a{width:100%;width:-moz-fit-content}`},
		{"height fallback", `.a{height:100vh;height:-webkit-fill-available}`, `.a{height:100vh;height:-webkit-fill-available}`},
		{"dvh fallback", `.a{height:100vh;height:100dvh}`, `.a{height:100vh;height:100dvh}`},
		{"p3 fallback", `.a{color:red;color:color(display-p3 1 0 0)}`, `.a{color:red;color:color(display-p3 1 0 0)}`},
		{"easing fallback", `.a{transition-timing-function:cubic-bezier(0.1,0.7,1,0.1);transition-timing-function:linear(0,1)}`, `.a{transition-timing-function:cubic-bezier(0.1,0.7,1,0.1);transition-timing-function:linear(0,1)}`},
		{"display fallback", `.a{display:block;display:grid}`, `.a{display:block;display:grid}`},
		{"var fallback", `.a{color:red;color:var(--c)}`, `.a{color:red;color:var(--c)}`},
		{"plain colors fold", `.a{color:red;color:blue}`, `.a{color:blue}`},
		{"px fold", `.a{width:10px;width:20px}`, `.a{width:20px}`},
		{"identical fold", `.a{width:calc(1px + var(--a));color:red;width:calc(1px + var(--a))}`, `.a{color:red;width:calc(1px + var(--a))}`},

		// 7: statements in leaf at-rule bodies pass through.
		{"layer order in media", `@media screen{@layer base,components;}`, `@media screen{@layer base,components}`},
		{"layer order nested", `@layer outer{@layer b, a;}`, `@layer outer{@layer b, a}`},

		// 8: !important is ignored in keyframes.
		{"keyframes important", `@keyframes k{from{opacity:0!important;opacity:1}}`, `@keyframes k{from{opacity:1}}`},
		{"normal important kept", `.a{opacity:0!important;opacity:1}`, `.a{opacity:0!important}`},

		// 9: "!important" inside a string is not important.
		{"important in string", `.a{content:"!important";content:"x"}`, `.a{content:"x"}`},

		// 10: comments.
		{"comment star selector", `ul>*/* c */{color:red}`, `ul>* {color:red}`},
		{"comment between tokens", `.a{margin:1px/**/2px}`, `.a{margin:1px 2px}`},

		// 11: escapes.
		{"escaped quote", `.content-\[\'x\'\]{color:red}.b{color:red;color:blue}`, `.content-\[\'x\'\]{color:red}.b{color:blue}`},
		{"escaped brace", `.a\{{color:red}.b{color:red;color:blue}`, `.a\{{color:red}.b{color:blue}`},

		// 13: seen by the fuzzer.
		{"bare token", `{0}`, `{0}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Minify(tt.in)
			if got != tt.want {
				t.Errorf("Minify(%q)\n got  %q\n want %q", tt.in, got, tt.want)
			}
			if again := Minify(got); again != got {
				t.Errorf("not idempotent: %q -> %q", got, again)
			}
		})
	}
}

func braceBalance(s string) (bal int, ok bool) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"', '\'':
			i = cssSkipString(s, i) - 1
		case '/':
			if i+1 < len(s) && s[i+1] == '*' {
				i = skipCSSComment(s, i) - 1
			}
		case '{':
			bal++
		case '}':
			bal--
			if bal < 0 {
				return bal, false
			}
		}
	}
	return bal, true
}

func FuzzMinify(f *testing.F) {
	seeds := []string{
		`{0}`, `.a{color:red;color:blue}`, `@media screen{.a{}}.b{color:red}`,
		`.a{color:red;&:hover{}}`, `@keyframes k{0%{}100%{opacity:1}}`,
		`.a{width:calc(1 * 10px + 5px)}`, `.a{background-color:red;background:blue;background-color:green}`,
		`@media screen{@layer base,components;}`, `.a\{{b:c}`, `ul>*/* c */{color:red}`,
		`.a{content:"}";margin:0px}`, `@layer a,b;.x{y:z}`, `.a{--x:0px;width:calc(0 - var(--x))}`,
		`.a{width:100%;width:-moz-fit-content}`, `/*! keep */.a{b:c}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := Minify(in)
		if again := Minify(out); again != out {
			t.Fatalf("not idempotent:\n in    %q\n out   %q\n again %q", in, out, again)
		}
		inBal, inOK := braceBalance(in)
		if inOK && inBal == 0 {
			outBal, outOK := braceBalance(out)
			if !outOK || outBal != 0 {
				t.Fatalf("brace balance broken:\n in  %q\n out %q", in, out)
			}
			if strings.HasPrefix(strings.TrimSpace(out), "}") {
				t.Fatalf("stray top-level close brace: %q", out)
			}
		}
	})
}
