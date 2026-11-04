package css

import "testing"

func TestNormalizeArbitrary(t *testing.T) {
	tests := map[string]string{
		"calc(100%-2rem)":           "calc(100% - 2rem)",
		"calc(100vh-4rem)":          "calc(100vh - 4rem)",
		"calc(100%_-_2rem)":         "calc(100% - 2rem)",
		"calc(var(--a-b)*2)":        "calc(var(--a-b) * 2)",
		"calc(-1*var(--x))":         "calc(-1 * var(--x))",
		"calc(1px+-2px)":            "calc(1px + -2px)",
		"calc((100%-2rem)/3)":       "calc((100% - 2rem) / 3)",
		"calc(100%/3-2*1rem)":       "calc(100% / 3 - 2 * 1rem)",
		"min(10px,5vw)":             "min(10px, 5vw)",
		"clamp(1rem,2vw,3rem)":      "clamp(1rem, 2vw, 3rem)",
		"0_auto":                    "0 auto",
		"url(/a_b.png)":             "url(/a_b.png)",
		"url(/a\\_b.png)":           "url(/a\\_b.png)",
		"var(--a-b,10px)":           "var(--a-b,10px)",
		"1fr_2fr":                   "1fr 2fr",
		"repeat(2,minmax(0,1fr))":   "repeat(2,minmax(0,1fr))",
		"a\\_b":                     "a_b",
		"'a_b'":                     "'a b'",
		"url('/x_y.png')_no-repeat": "url('/x_y.png') no-repeat",
	}
	for in, want := range tests {
		if got := normalizeArbitrary(in); got != want {
			t.Errorf("normalizeArbitrary(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParenToBracket(t *testing.T) {
	tests := map[string]string{
		"w-(--w)":                "w-[var(--w)]",
		"text-(length:--s)":      "text-[length:var(--s)]",
		"font-(family-name:--f)": "font-[family:var(--f)]",
		"text-red-500/(--alpha)": "text-red-500/[var(--alpha)]",
		"w-[calc(1px+2px)]":      "w-[calc(1px+2px)]",
		"bg-[url(/x.png)]":       "bg-[url(/x.png)]",
		"flex":                   "flex",
		"w-(foo)":                "w-(foo)",
	}
	for in, want := range tests {
		if got := parenToBracket(in); got != want {
			t.Errorf("parenToBracket(%q) = %q, want %q", in, got, want)
		}
	}
}
