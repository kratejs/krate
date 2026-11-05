package css

// TailwindTheme holds user-customizable theme values for the Tailwind generator.
type TailwindTheme struct {
	Spacing     map[string]string            `json:"spacing"`
	Sizing      map[string]string            `json:"sizing"`
	MaxWidth    map[string]string            `json:"maxWidth"`
	MinWidth    map[string]string            `json:"minWidth"`
	LineHeight  map[string]string            `json:"lineHeight"`
	Opacity     map[string]string            `json:"opacity"`
	Screens     map[string]string            `json:"screens"`
	Colors      map[string]map[string]string `json:"colors"`
	TextSizes   map[string]string            `json:"textSizes"`
	FontWeights map[string]string            `json:"fontWeights"`
	FontFamily  map[string]string            `json:"fontFamily"`
	Radii       map[string]string            `json:"radii"`
	Shadows     map[string]string            `json:"shadows"`
	BgColors    map[string]string            `json:"bgColors"`
	// DarkMode is "media" (default), "class", or "selector".
	DarkMode string `json:"darkMode"`
	// DarkSelector is the selector of class/selector dark mode
	// (default ".dark"), e.g. "[data-mode=dark]".
	DarkSelector string `json:"darkSelector"`

	// Animation maps an animate-<name> utility to its animation value
	// (theme.animation, @theme --animate-*).
	Animation map[string]string `json:"animation"`
	// Keyframes maps a keyframes name to its block body without the
	// @keyframes wrapper, e.g. "to{transform:rotate(360deg)}".
	Keyframes map[string]string `json:"keyframes"`
	// LiveVars names the theme variables (no leading "--") that utilities read
	// through var() instead of inlining, so a runtime override takes effect.
	LiveVars map[string]bool `json:"-"`
	// Tracking maps tracking-<name> to a letter-spacing value.
	Tracking map[string]string `json:"tracking"`
	// Containers maps a container size name (@sm, max-w-sm in v4) to a width.
	Containers map[string]string `json:"containers"`
	// SpacingUnit is the v4 --spacing base unit (default 0.25rem) used for
	// numeric spacing multiples.
	SpacingUnit string `json:"spacingUnit"`
	// Prefix is the configured class prefix (v4 "tw" -> tw:flex), or "".
	Prefix string `json:"prefix"`
	// Important makes every utility !important when "true"; any other
	// non-empty value is a selector utilities are scoped under.
	Important string `json:"important"`
	// CustomUtilities maps a static `@utility name { ... }` to its declarations.
	CustomUtilities map[string]string `json:"customUtilities"`
	// FunctionalUtilities maps the prefix of a `@utility name-* { ... }` to its
	// declarations, whose --value()/--spacing() calls resolve per class.
	FunctionalUtilities map[string]string `json:"functionalUtilities,omitempty"`
	// CustomVariants maps `@custom-variant name (...)` to a selector template
	// containing "&" (e.g. "&:where([data-t=x] *)") or an at-rule such as
	// "@media (print)".
	CustomVariants map[string]string `json:"customVariants"`
	// ThemeVars holds every `@theme` variable by name without the leading
	// `--`, so `--value(--tab-size-*)` can read namespaces Krate has no
	// utilities for.
	ThemeVars map[string]string `json:"themeVars,omitempty"`

	// utilStack lists the custom utilities being expanded, to stop @apply
	// cycles between them.
	utilStack []string
}

// DefaultTailwindTheme returns the default Tailwind theme configuration.
func DefaultTailwindTheme() TailwindTheme {
	return TailwindTheme{
		Spacing:    defaultSpacing(),
		Sizing:     defaultSizing(),
		MaxWidth:   defaultMaxWidth(),
		MinWidth:   defaultMinWidth(),
		LineHeight: defaultLineHeight(),
		Opacity:    defaultOpacity(),
		Screens:    defaultScreens(),
		FontFamily: defaultFontFamily(),
		Colors:     defaultColors(),
		DarkMode:   "media",
		TextSizes: map[string]string{
			"xs":   "font-size: 0.75rem; line-height: 1rem;",
			"sm":   "font-size: 0.875rem; line-height: 1.25rem;",
			"base": "font-size: 1rem; line-height: 1.5rem;",
			"lg":   "font-size: 1.125rem; line-height: 1.75rem;",
			"xl":   "font-size: 1.25rem; line-height: 1.75rem;",
			"2xl":  "font-size: 1.5rem; line-height: 2rem;",
			"3xl":  "font-size: 1.875rem; line-height: 2.25rem;",
			"4xl":  "font-size: 2.25rem; line-height: 2.5rem;",
			"5xl":  "font-size: 3rem; line-height: 1;",
			"6xl":  "font-size: 3.75rem; line-height: 1;",
			"7xl":  "font-size: 4.5rem; line-height: 1;",
			"8xl":  "font-size: 6rem; line-height: 1;",
			"9xl":  "font-size: 8rem; line-height: 1;",
		},
		FontWeights: map[string]string{
			"thin":       "100",
			"extralight": "200",
			"light":      "300",
			"normal":     "400",
			"medium":     "500",
			"semibold":   "600",
			"bold":       "700",
			"extrabold":  "800",
			"black":      "900",
		},
		Radii: map[string]string{
			"none": "0",
			"xs":   "0.125rem",
			"sm":   "0.25rem",
			"":     "0.25rem",
			"md":   "0.375rem",
			"lg":   "0.5rem",
			"xl":   "0.75rem",
			"2xl":  "1rem",
			"3xl":  "1.5rem",
			"4xl":  "2rem",
			"full": "9999px",
		},
		Shadows: defaultShadows(),
		BgColors: map[string]string{
			"transparent": "transparent",
			"current":     "currentColor",
			"white":       "#fff",
			"black":       "#000",
			"inherit":     "inherit",
		},
	}
}

// defaultScreens returns the standard responsive breakpoints (min-width).
func defaultScreens() map[string]string {
	return map[string]string{
		"sm": "640px", "md": "768px", "lg": "1024px", "xl": "1280px", "2xl": "1536px",
	}
}

// defaultMaxWidth mirrors Tailwind's max-width scale.
func defaultMaxWidth() map[string]string {
	return map[string]string{
		"none": "none", "0": "0rem", "xs": "20rem", "sm": "24rem", "md": "28rem",
		"lg": "32rem", "xl": "36rem", "2xl": "42rem", "3xl": "48rem", "4xl": "56rem",
		"5xl": "64rem", "6xl": "72rem", "7xl": "80rem", "full": "100%",
		"min": "min-content", "max": "max-content", "fit": "fit-content",
		"prose": "65ch", "screen-sm": "640px", "screen-md": "768px",
		"screen-lg": "1024px", "screen-xl": "1280px", "screen-2xl": "1536px",
	}
}

// defaultMinWidth mirrors Tailwind's min-width scale.
func defaultMinWidth() map[string]string {
	return map[string]string{
		"0": "0px", "full": "100%", "min": "min-content", "max": "max-content",
		"fit": "fit-content",
	}
}

// defaultLineHeight mirrors Tailwind's named leading scale.
func defaultLineHeight() map[string]string {
	return map[string]string{
		"none": "1", "tight": "1.25", "snug": "1.375", "normal": "1.5",
		"relaxed": "1.625", "loose": "2",
	}
}

// defaultOpacity mirrors Tailwind's opacity scale.
func defaultOpacity() map[string]string {
	return map[string]string{
		"0": "0", "5": "0.05", "10": "0.1", "15": "0.15", "20": "0.2",
		"25": "0.25", "30": "0.3", "35": "0.35", "40": "0.4", "45": "0.45",
		"50": "0.5", "55": "0.55", "60": "0.6", "65": "0.65", "70": "0.7",
		"75": "0.75", "80": "0.8", "85": "0.85", "90": "0.9", "95": "0.95",
		"100": "1",
	}
}

// defaultFontFamily mirrors Tailwind's font stacks.
func defaultFontFamily() map[string]string {
	return map[string]string{
		"sans":  "-apple-system, BlinkMacSystemFont, \"Segoe UI\", Roboto, \"Helvetica Neue\", \"Noto Sans\", Arial, sans-serif, \"Apple Color Emoji\", \"Segoe UI Emoji\", \"Segoe UI Symbol\", \"Noto Color Emoji\"",
		"serif": "ui-serif, Georgia, Cambria, \"Times New Roman\", Times, serif",
		"mono":  "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, \"Liberation Mono\", \"Courier New\", monospace",
	}
}

func defaultSpacing() map[string]string {
	return map[string]string{
		"0":   "0",
		"px":  "1px",
		"0.5": "0.125rem",
		"1":   "0.25rem",
		"1.5": "0.375rem",
		"2":   "0.5rem",
		"2.5": "0.625rem",
		"3":   "0.75rem",
		"3.5": "0.875rem",
		"4":   "1rem",
		"5":   "1.25rem",
		"6":   "1.5rem",
		"7":   "1.75rem",
		"8":   "2rem",
		"9":   "2.25rem",
		"10":  "2.5rem",
		"11":  "2.75rem",
		"12":  "3rem",
		"14":  "3.5rem",
		"16":  "4rem",
		"20":  "5rem",
		"24":  "6rem",
		"28":  "7rem",
		"32":  "8rem",
		"36":  "9rem",
		"40":  "10rem",
		"44":  "11rem",
		"48":  "12rem",
		"52":  "13rem",
		"56":  "14rem",
		"60":  "15rem",
		"64":  "16rem",
		"72":  "18rem",
		"80":  "20rem",
		"96":  "24rem",
	}
}

func defaultSizing() map[string]string {
	return map[string]string{
		"0":    "0",
		"px":   "1px",
		"0.5":  "0.125rem",
		"1":    "0.25rem",
		"1.5":  "0.375rem",
		"2":    "0.5rem",
		"2.5":  "0.625rem",
		"3":    "0.75rem",
		"3.5":  "0.875rem",
		"4":    "1rem",
		"5":    "1.25rem",
		"6":    "1.5rem",
		"7":    "1.75rem",
		"8":    "2rem",
		"9":    "2.25rem",
		"10":   "2.5rem",
		"11":   "2.75rem",
		"12":   "3rem",
		"14":   "3.5rem",
		"16":   "4rem",
		"20":   "5rem",
		"24":   "6rem",
		"28":   "7rem",
		"32":   "8rem",
		"36":   "9rem",
		"40":   "10rem",
		"44":   "11rem",
		"48":   "12rem",
		"52":   "13rem",
		"56":   "14rem",
		"60":   "15rem",
		"64":   "16rem",
		"72":   "18rem",
		"80":   "20rem",
		"96":   "24rem",
		"auto": "auto",
		"1/2":  "50%",
		"1/3":  "33.333333%",
		"2/3":  "66.666667%",
		"1/4":  "25%",
		"3/4":  "75%",
		"1/5":  "20%",
		"2/5":  "40%",
		"3/5":  "60%",
		"4/5":  "80%",
		"1/6":  "16.666667%",
		"5/6":  "83.333333%",
		"full": "100%",
	}
}

// defaultShadows is the v4 box-shadow scale (shadow == shadow-sm).
func defaultShadows() map[string]string {
	m := map[string]string{"none": "none"}
	for k, v := range boxShadowScale {
		m[k] = v
	}
	return m
}
