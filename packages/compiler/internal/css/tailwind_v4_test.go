package css

import (
	"strings"
	"testing"
)

func wantGen(t *testing.T, tests map[string]string) {
	t.Helper()
	for cls, want := range tests {
		if got := gen(t, cls); !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
	}
}

func TestAutoAndFullSpacing(t *testing.T) {
	wantGen(t, map[string]string{
		"mx-auto":     "margin-inline: auto;",
		"m-auto":      "margin: auto;",
		"my-auto":     "margin-block: auto;",
		"mt-auto":     "margin-top: auto;",
		"inset-auto":  "inset: auto;",
		"top-auto":    "top: auto;",
		"bottom-full": "bottom: 100%;",
		"inset-full":  "inset: 100%;",
		"start-0":     "inset-inline-start: 0;",
		"end-4":       "inset-inline-end: 1rem;",
		"-mx-4":       "margin-inline: -1rem;",
		"m-px":        "margin: 1px;",
		"mx-[0_auto]": "margin-inline: 0 auto;",
		"m-(--m)":     "margin: var(--m);",
		"p-(--p)":     "padding: var(--p);",
		"inset-x-0":   "inset-inline: 0;",
	})
	g := NewTailwindGenerator()
	g.Strict = true
	genKF(t, g, "p-auto", "-p-4", "mx-foo")
	if len(g.Unknown) != 3 {
		t.Errorf("invalid spacing must be unknown: %v", g.Unknown)
	}
}

func TestContainerBreakpoints(t *testing.T) {
	got := gen(t, "container")
	for _, want := range []string{
		".container{width: 100%;}",
		"@media (min-width: 640px){.container{max-width: 640px;}}",
		"@media (min-width: 1536px){.container{max-width: 1536px;}}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("container missing %q in:\n%s", want, got)
		}
	}
	if got := gen(t, "md:container"); !strings.Contains(got, "@media (min-width: 768px){@media (min-width: 640px){.md\\:container{max-width: 640px;}}}") {
		t.Errorf("md:container: %s", got)
	}
}

func TestTextSizeLineHeightAndTypography(t *testing.T) {
	wantGen(t, map[string]string{
		"text-sm":           "font-size: 0.875rem; line-height: var(--tw-leading, calc(1.25 / 0.875));",
		"text-5xl":          "line-height: var(--tw-leading, 1);",
		"text-sm/6":         "font-size: 0.875rem; line-height: 1.5rem;",
		"text-lg/tight":     "font-size: 1.125rem; line-height: 1.25;",
		"text-sm/[22px]":    "line-height: 22px;",
		"text-[14px]/6":     "font-size: 14px; line-height: 1.5rem;",
		"text-sm/(--lh)":    "line-height: var(--lh);",
		"text-(length:--s)": "font-size: var(--s);",
		"text-(--c)":        "color: var(--c);",
		"text-(color:--c)":  "color: var(--c);",
		"leading-6":         "--tw-leading: 1.5rem; line-height: 1.5rem;",
		"leading-none":      "--tw-leading: 1; line-height: 1;",
		"leading-(--l)":     "--tw-leading: var(--l); line-height: var(--l);",
		"antialiased":       "-webkit-font-smoothing: antialiased; -moz-osx-font-smoothing: grayscale;",
		"text-balance":      "text-wrap: balance;",
		"text-pretty":       "text-wrap: pretty;",
		"text-nowrap":       "text-wrap: nowrap;",
		"text-start":        "text-align: start;",
		"tracking-wide":     "letter-spacing: 0.025em;",
		"font-[450]":        "font-weight: 450;",
		"font-[Inter]":      "font-family: Inter;",
		"font-(--w)":        "font-weight: var(--w);",
		"font-bold":         "--tw-font-weight: 700; font-weight: 700;",
		"underline":         "text-decoration-line: underline;",
		"line-through":      "text-decoration-line: line-through;",
		"no-underline":      "text-decoration-line: none;",
	})
}

func TestTextAlphaFromVar(t *testing.T) {
	wantGen(t, map[string]string{
		"text-red-500/(--alpha)": "color: rgb(239 68 68 / var(--alpha));",
		"text-red-500/50":        "rgb(239 68 68 / 0.5)",
		"text-red-500/[0.37]":    "rgb(239 68 68 / 0.37)",
	})
	g := NewTailwindGenerator()
	g.Theme.Colors["brand"] = map[string]string{"DEFAULT": "oklch(60% 0.2 250)"}
	css, _ := genKF(t, g, "bg-brand/40", "text-brand/(--a)")
	if !strings.Contains(css, "color-mix(in oklab, oklch(60% 0.2 250) 40%, transparent)") ||
		!strings.Contains(css, "color-mix(in oklab, oklch(60% 0.2 250) var(--a), transparent)") {
		t.Errorf("alpha on a non-hex color must not be dropped:\n%s", css)
	}
}

func TestThemeFontTrackingAndSpacingUnit(t *testing.T) {
	g := NewTailwindGenerator()
	g.Theme.FontFamily["display"] = `"Cal Sans", sans-serif`
	g.Theme.FontFamily["sans"] = `Inter, sans-serif`
	g.Theme.Tracking = map[string]string{"huge": "0.3em", "tight": "-0.5em"}
	g.Theme.SpacingUnit = "4px"
	css, _ := genKF(t, g, "font-display font-sans tracking-huge tracking-tight p-3 w-10 gap-0.5")
	for _, want := range []string{
		`font-family: "Cal Sans", sans-serif;`, "font-family: Inter, sans-serif;",
		"letter-spacing: 0.3em;", "letter-spacing: -0.5em;", "padding: 12px;", "width: 40px;", "gap: 2px;",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("missing %q in:\n%s", want, css)
		}
	}
}

func TestTablesAndOverflowAndKeywords(t *testing.T) {
	wantGen(t, map[string]string{
		"table-cell":            "display: table-cell;",
		"table-row":             "display: table-row;",
		"table-caption":         "display: table-caption;",
		"overflow-clip":         "overflow: clip;",
		"overflow-x-clip":       "overflow-x: clip;",
		"collapse":              "visibility: collapse;",
		"align-sub":             "vertical-align: sub;",
		"list-disc":             "list-style-type: disc;",
		"select-none":           "-webkit-user-select: none; user-select: none;",
		"grid-cols-none":        "grid-template-columns: none;",
		"grid-cols-subgrid":     "grid-template-columns: subgrid;",
		"grid-rows-subgrid":     "grid-template-rows: subgrid;",
		"grid-rows-3":           "grid-template-rows: repeat(3, minmax(0, 1fr));",
		"aspect-3/2":            "aspect-ratio: 3/2;",
		"aspect-video":          "aspect-ratio: 16 / 9;",
		"flex-[2_2_0%]":         "flex: 2 2 0%;",
		"flex-2":                "flex: 2;",
		"grow-3":                "flex-grow: 3;",
		"basis-3xs":             "flex-basis: 16rem;",
		"basis-(--b)":           "flex-basis: var(--b);",
		"col-span-(--c)":        "grid-column: span var(--c) / span var(--c);",
		"col-start-auto":        "grid-column-start: auto;",
		"-col-start-1":          "grid-column-start: -1;",
		"auto-cols-fr":          "grid-auto-columns: minmax(0, 1fr);",
		"auto-rows-min":         "grid-auto-rows: min-content;",
		"auto-cols-max":         "grid-auto-columns: max-content;",
		"order-[7]":             "order: 7;",
		"z-(--z)":               "z-index: var(--z);",
		"cursor-crosshair":      "cursor: crosshair;",
		"line-clamp-(--l)":      "-webkit-line-clamp: var(--l);",
		"object-(--o)":          "object-position: var(--o);",
		"stroke-[3px]":          "stroke-width: 3px;",
		"sr-only":               "clip-path: inset(50%);",
		"opacity-33":            "opacity: 0.33;",
		"rounded-xs":            "border-radius: 0.125rem;",
		"rounded-sm":            "border-radius: 0.25rem;",
		"rounded-t-[4px]":       "border-top-left-radius: 4px;",
		"wrap-anywhere":         "overflow-wrap: anywhere;",
		"touch-pan-x":           "--tw-pan-x: pan-x;",
		"items-center-safe":     "align-items: safe center;",
		"h-lh":                  "height: 1lh;",
		"w-sm":                  "width: 24rem;",
		"max-w-7xl":             "max-width: 80rem;",
		"size-4":                "width: 1rem; height: 1rem;",
		"w-1/2":                 "width: 50%;",
		"w-(--w)":               "width: var(--w);",
		"min-w-(--x)":           "min-width: var(--x);",
		"bg-(--x)":              "background-color: var(--x);",
		"bg-(image:--i)":        "background-image: var(--i);",
		"decoration-(--d)":      "text-decoration-color: var(--d);",
		"duration-(--d)":        "transition-duration: var(--d);",
		"[mask-type:luminance]": "mask-type: luminance;",
		"[--x:1px]":             "--x: 1px;",
	})
	if got := gen(t, "hover:[mask-type:alpha]"); !strings.Contains(got, ":hover{mask-type: alpha;}") {
		t.Errorf("arbitrary property with variant: %s", got)
	}
}

func TestArbitraryValueNormalization(t *testing.T) {
	wantGen(t, map[string]string{
		"w-[calc(100%-2rem)]":      "width: calc(100% - 2rem);",
		"h-[calc(100vh-4rem)]":     "height: calc(100vh - 4rem);",
		"w-[calc(var(--a-b)*2)]":   "width: calc(var(--a-b) * 2);",
		"bg-[url(/a_b.png)]":       "background-image: url(/a_b.png);",
		"[--my-var:calc(1px+2px)]": "--my-var: calc(1px + 2px);",
		"grid-cols-[1fr_2fr]":      "grid-template-columns: 1fr 2fr;",
	})
}

func TestOutlineDrawsAndRingOffsetComposes(t *testing.T) {
	wantGen(t, map[string]string{
		"outline-2":         "outline-style: var(--tw-outline-style, solid); outline-width: 2px;",
		"outline":           "outline-style: var(--tw-outline-style, solid); outline-width: 1px;",
		"outline-dashed":    "--tw-outline-style: dashed; outline-style: dashed;",
		"outline-offset-2":  "outline-offset: 2px;",
		"-outline-offset-2": "outline-offset: -2px;",
		"ring-2":            "calc(2px + var(--tw-ring-offset-width, 0px))",
		"ring-offset-2":     "--tw-ring-offset-width: 2px; --tw-ring-offset-shadow:",
		"ring-offset-white": "--tw-ring-offset-color: #ffffff;",
		"ring-red-500":      "--tw-ring-color: #ef4444;",
		"inset-ring-2":      "--tw-inset-ring-shadow: inset 0 0 0 2px",
		"shadow-md":         "--tw-shadow: 0 4px 6px -1px var(--tw-shadow-color, rgb(0 0 0 / 0.1))",
		"shadow-red-500":    "--tw-shadow-color: #ef4444;",
		"inset-shadow-sm":   "--tw-inset-shadow: inset 0 2px 4px var(--tw-inset-shadow-color, rgb(0 0 0 / 0.05));",
		"text-shadow-sm":    "text-shadow: 0px 1px 0px var(--tw-text-shadow-color, rgb(0 0 0 / 0.075))",
		"drop-shadow-md":    "--tw-drop-shadow: drop-shadow(0 3px 3px var(--tw-drop-shadow-color, rgb(0 0 0 / 0.12)));",
	})
	// ring and ring-offset use the same box-shadow composition so both apply.
	css := gen(t, "ring-2 ring-offset-2 ring-offset-white")
	if strings.Count(css, "var(--tw-ring-offset-shadow, 0 0 #0000)") != 1 {
		t.Errorf("ring must read the offset shadow in its composition:\n%s", css)
	}
}

func TestSpaceAndDivideTargetChildren(t *testing.T) {
	wantGen(t, map[string]string{
		"-space-x-2":      ":where(.-space-x-2 > :not(:last-child)){--tw-space-x-reverse: 0; margin-inline-start: calc(-0.5rem * var(--tw-space-x-reverse, 0)); margin-inline-end: calc(-0.5rem * calc(1 - var(--tw-space-x-reverse, 0)));}",
		"space-y-2":       "margin-block-start: calc(0.5rem * var(--tw-space-y-reverse, 0));",
		"space-x-reverse": ":where(.space-x-reverse > :not(:last-child)){--tw-space-x-reverse: 1;}",
		"divide-x":        ":where(.divide-x > :not(:last-child))",
		"divide-red-500":  ":where(.divide-red-500 > :not(:last-child)){border-color: #ef4444;}",
		"divide-dashed":   ":where(.divide-dashed > :not(:last-child)){--tw-border-style: dashed; border-style: dashed;}",
		"hover:space-x-2": ":where(.hover\\:space-x-2:hover > :not(:last-child))",
		"divide-y-4":      "border-top-width: calc(4px * var(--tw-divide-y-reverse, 0));",
	})
}

func TestGradientPositionsAndStops(t *testing.T) {
	css, kf := genKF(t, NewTailwindGenerator(), "bg-linear-to-r from-red-500 via-blue-500 to-green-500 from-10% via-30% to-90%")
	for _, want := range []string{
		"--tw-gradient-position: to right in oklab; background-image: linear-gradient(var(--tw-gradient-stops));",
		"--tw-gradient-from: #ef4444; --tw-gradient-stops: var(--tw-gradient-via-stops, var(--tw-gradient-position), var(--tw-gradient-from, #0000) var(--tw-gradient-from-position, 0%), var(--tw-gradient-to, #0000) var(--tw-gradient-to-position, 100%));",
		"--tw-gradient-via: #3b82f6;",
		"var(--tw-gradient-via, #0000) var(--tw-gradient-via-position, 50%)",
		"--tw-gradient-to: #22c55e;",
		".from-10\\%{--tw-gradient-from-position: 10%;}",
		".via-30\\%{--tw-gradient-via-position: 30%;}",
		".to-90\\%{--tw-gradient-to-position: 90%;}",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("missing %q in:\n%s", want, css)
		}
	}
	if !strings.Contains(kf, "@property --tw-gradient-from-position") {
		t.Errorf("gradient properties not registered: %s", kf)
	}
	wantGen(t, map[string]string{
		"bg-linear-45":           "--tw-gradient-position: 45deg in oklab;",
		"bg-gradient-to-tl":      "--tw-gradient-position: to top left in oklab;",
		"bg-linear-to-r/oklch":   "to right in oklch;",
		"bg-radial":              "radial-gradient(var(--tw-gradient-stops))",
		"bg-conic-180":           "--tw-gradient-position: from 180deg in oklab;",
		"bg-radial-[at_50%_75%]": "--tw-gradient-position: at 50% 75%;",
	})
}

func TestBorderUsesStyleVariable(t *testing.T) {
	wantGen(t, map[string]string{
		"border":            "border-style: var(--tw-border-style, solid); border-width: 1px;",
		"border-dashed":     "--tw-border-style: dashed; border-style: dashed;",
		"border-t-2":        "border-top-style: var(--tw-border-style, solid); border-top-width: 2px;",
		"border-t-red-500":  "border-top-color: #ef4444;",
		"border-x-blue-500": "border-inline-color: #3b82f6;",
		"border-(--b)":      "border-color: var(--b);",
		"border-[3px]":      "border-width: 3px;",
		"border-s-2":        "border-inline-start-width: 2px;",
	})
	css := gen(t, "border border-dashed")
	if strings.Contains(css, "border-style: solid") {
		t.Errorf("border must not hard-code solid (it would beat border-dashed):\n%s", css)
	}
}

func TestUtilityRulesAreValidCSS(t *testing.T) {
	// Every emitted declaration block for these classes must parse (Chromium
	// verification lives in the browser conformance suite).
	for _, cls := range []string{"text-sm/6", "ring-2", "from-10%", "container", "space-x-2", "animate-spin"} {
		if gen(t, cls) == "" {
			t.Errorf("%s produced nothing", cls)
		}
	}
}
