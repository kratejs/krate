package build

import (
	"strings"
	"testing"
)

// TestCSSSignalContract is a golden contract across the primitives and ARIA
// roles: controller types, label wiring, zero-JS pages, and the no-:has()
// fallback.
func TestCSSSignalContract(t *testing.T) {
	cases := []struct {
		name         string
		src          string
		wantRadio    int
		wantCheckbox int
		wantARIA     bool
	}{
		{
			name:      "choice",
			src:       `export default function P(){const v=createCSSChoice('a',['a','b']);return <div><button {...v.trigger('a')}>A</button><button {...v.trigger('b')}>B</button><div {...v.panel('a')}>A</div><div {...v.panel('b')}>B</div></div>;}`,
			wantRadio: 2,
		},
		{
			name:      "choice-tabs-aria",
			src:       `export default function P(){const v=createCSSChoice('a',{as:'tabs',options:['a','b']});return <div><button {...v.trigger('a')}>A</button><button {...v.trigger('b')}>B</button><div {...v.panel('a')}>A</div><div {...v.panel('b')}>B</div></div>;}`,
			wantRadio: 2,
			wantARIA:  true,
		},
		{
			name:         "toggle",
			src:          `export default function P(){const v=createCSSToggle(false);return <div><button {...v.trigger()}>T</button><div {...v.panel(true)}>On</div><div {...v.panel(false)}>Off</div></div>;}`,
			wantCheckbox: 1,
		},
		{
			name:         "flags",
			src:          `export default function P(){const v=createCSSFlags(['x','y']);return <div><button {...v.trigger('x')}>X</button><button {...v.trigger('y')}>Y</button></div>;}`,
			wantCheckbox: 2,
		},
		{
			name:      "group",
			src:       `export default function P(){const v=createCSSGroup(null,{as:'accordion'});return <div><button {...v.trigger('a')}>A</button><div {...v.panel('a')}>A</div><button {...v.close()}>Close</button></div>;}`,
			wantRadio: 2, // option + closed sentinel
		},
		{
			name:      "range",
			src:       `export default function P(){const v=createCSSRange(2,{min:0,max:4,step:1});return <div><button {...v.trigger(0)}>0</button><button {...v.trigger(4)}>4</button><div class="fill"></div></div>;}`,
			wantRadio: 5,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			html, outDir, err := buildPageSrc(t, tc.src)
			if err != nil {
				t.Fatalf("BuildAll: %v", err)
			}
			if got := strings.Count(html, `type=radio`) + strings.Count(html, `type="radio"`); got != tc.wantRadio {
				t.Errorf("radio controllers: got %d, want %d\n%.1200s", got, tc.wantRadio, html)
			}
			if got := strings.Count(html, `type=checkbox`) + strings.Count(html, `type="checkbox"`); got != tc.wantCheckbox {
				t.Errorf("checkbox controllers: got %d, want %d\n%.1200s", got, tc.wantCheckbox, html)
			}
			if hasHydrationScript(html) {
				t.Errorf("%s should ship no page bundle:\n%.1200s", tc.name, html)
			}
			assertForResolves(t, html)
			if tc.wantARIA && !strings.Contains(html, "aria-selected") {
				t.Errorf("%s should inject the ARIA synchroniser:\n%.1200s", tc.name, html)
			}
			css := readAllCSS(t, outDir)
			if !strings.Contains(css, "@supports not selector(:has(*))") {
				t.Errorf("%s: missing no-:has() fallback:\n%s", tc.name, css)
			}
			if !strings.Contains(css, "display:none") || !strings.Contains(css, "display:contents") {
				t.Errorf("%s: expected hide/show rules:\n%s", tc.name, css)
			}
		})
	}
}

// TestCSSSignalDeterministic verifies the generated HTML and CSS are
// byte-identical across repeated builds of the same source.
func TestCSSSignalDeterministic(t *testing.T) {
	src := `export default function P(){const v=createCSSChoice('a',['a','b']);return <div><button {...v.trigger('a')}>A</button><button {...v.trigger('b')}>B</button><div {...v.panel('a')}>A</div><div {...v.panel('b')}>B</div></div>;}`
	html1, out1, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatal(err)
	}
	html2, out2, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if html1 != html2 {
		t.Errorf("HTML differs between builds")
	}
	if c1, c2 := readAllCSS(t, out1), readAllCSS(t, out2); c1 != c2 {
		t.Errorf("CSS differs between builds")
	}
}

// FuzzCSSOptionValues feeds arbitrary option strings through a full build to
// catch panics or invalid output on hostile/unicode/empty option values.
func FuzzCSSOptionValues(f *testing.F) {
	for _, s := range []string{"a", "", "hover", "x:active", ">", "日本", "a b c", `"q"`, "0"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, opt string) {
		if len(opt) > 48 || strings.ContainsAny(opt, "'\\\n\r") {
			return // keep the generated source syntactically valid
		}
		src := "export default function P(){const v=createCSSChoice('" + opt + "',['" + opt + "']);" +
			"return <div><button {...v.trigger('" + opt + "')}>{v.current()}</button>" +
			"<div {...v.panel('" + opt + "')}>x</div></div>;}"
		html, _, err := buildPageSrc(t, src)
		if err != nil {
			return // a validation error is acceptable; a panic is not
		}
		if strings.Contains(html, "__children__") {
			t.Fatalf("placeholder leaked for option %q", opt)
		}
	})
}
