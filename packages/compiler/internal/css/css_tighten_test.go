package css

import "testing"

func TestMinifyTightensSpacing(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"between rules and before brace", `.a { color: red } .b { color: blue }`, `.a{color:red}.b{color:blue}`},
		{"combinators", `.a > .b + .c ~ .d { top: 1px }`, `.a>.b+.c~.d{top:1px}`},
		{"descendant kept", `.a   .b { top: 1px }`, `.a .b{top:1px}`},
		{"selector list commas", `.a , .b ,.c { top: 1px }`, `.a,.b,.c{top:1px}`},
		{"nested selector combinator", `.a { & > .b { top: 1px } }`, `.a{&>.b{top:1px}}`},
		{"is where not", `:is(.a, .b) > :not(.c , .d) { top: 1px }`, `:is(.a,.b)>:not(.c,.d){top:1px}`},
		{"is with combinator", `:is(.a > .b) { top: 1px }`, `:is(.a>.b){top:1px}`},
		{"nth-child keeps its plus", `li:nth-child(2n + 1) { top: 1px }`, `li:nth-child(2n + 1){top:1px}`},
		{"attribute selector kept", `a[href="a > b, c"] > i, a[data-x=y] { top: 1px }`, `a[href="a > b, c"]>i,a[data-x=y]{top:1px}`},
		{"escaped combinator", `.a\+b > .c { top: 1px }`, `.a\+b>.c{top:1px}`},
		{"layer prelude", `@layer base , components; @layer base { .a { top: 1px } }`, `@layer base,components;@layer base{.a{top:1px}}`},
		{"media prelude", `@media (min-width: 768px) and (max-width: 1000px) , print { .a { top: 1px } }`, `@media (min-width:768px) and (max-width:1000px),print{.a{top:1px}}`},
		{"media range keeps its operators", `@media (width >= 600px) { .a { top: 1px } }`, `@media (width >= 600px){.a{top:1px}}`},
		{"supports selector untouched", `@supports selector(a > b) { .a { top: 1px } }`, `@supports selector(a > b){.a{top:1px}}`},
		{"container prelude", `@container card (min-width: 400px) { .a { top: 1px } }`, `@container card (min-width:400px){.a{top:1px}}`},
		{"value commas", `.a { font-family: "A B", serif , sans-serif; transition: color .2s ease , background .3s }`, `.a{font-family:"A B",serif,sans-serif;transition:color .2s ease,background .3s}`},
		{"gradient", `.a { background: linear-gradient( to right, #ffffff 0%, #000 100% ) }`, `.a{background:linear-gradient(to right,#fff 0%,#000 100%)}`},
		{"important", `.a { color: red !important; top: 1px ! important }`, `.a{color:red!important;top:1px!important}`},
		{"leading zeros", `.a { margin: 0.85rem -0.5rem 0.0px 10.5px; opacity: 0.5 }`, `.a{margin:.85rem -.5rem 0 10.5px;opacity:.5}`},
		{"leading zero inside functions", `.a { width: calc(100% - 0.5rem); color: rgba(0,0,0,0.5) }`, `.a{width:calc(100% - .5rem);color:rgba(0,0,0,.5)}`},
		{"calc keeps operator spaces", `.a { width: calc( 100% - 2rem + 1px ); height: min(10px, 5vw + 2px); margin: clamp(1rem , 2vw - 1px , 3rem) }`, `.a{width:calc(100% - 2rem + 1px);height:min(10px,5vw + 2px);margin:clamp(1rem,2vw - 1px,3rem)}`},
		{"custom property untouched", `.a { --x: a , b ; --y: 0.5 ; --z: calc( 1px + 2px ) !important }`, `.a{--x:a , b;--y:0.5;--z:calc( 1px + 2px ) !important}`},
		{"strings and urls untouched", `.a { content: "a , b > c  0.5"; background: url( "a , b.png" ) , url(c d.png) }`, `.a{content:"a , b > c  0.5";background:url( "a , b.png" ),url(c d.png)}`},
		{"url in value with important", `.a { background: url(x.png) !important }`, `.a{background:url(x.png)!important}`},
		{"declaration beside nested rule", `.a { color : red ; & > .b { top: 1px } }`, `.a{color:red;&>.b{top:1px}}`},
		{"keyframes", `@keyframes k { from , 50% { opacity: 0.5 } to { opacity: 1 } }`, `@keyframes k{from,50%{opacity:.5}to{opacity:1}}`},
		{"flex keeps zero unit", `.a { flex: 1 1 0px; flex-basis: 0.5px }`, `.a{flex:1 1 0px;flex-basis:0.5px}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Minify(c.in)
			if got != c.want {
				t.Errorf("Minify(%q)\n got  %q\n want %q", c.in, got, c.want)
			}
			if again := Minify(got); again != got {
				t.Errorf("not idempotent: %q -> %q", got, again)
			}
		})
	}
}
