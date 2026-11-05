package css

import "strings"

// TailwindPreflight returns Tailwind v4's Preflight base reset (opt-in via
// `tailwind.preflight`). The --tw-* composition variables are not set here:
// utilities carry inline fallbacks and KeyframesCSS registers them.
func TailwindPreflight(theme TailwindTheme) string {
	var b strings.Builder
	w := func(s string) { b.WriteString(s + "\n") }

	w("*,::after,::before,::backdrop,::file-selector-button{box-sizing:border-box;margin:0;padding:0;border:0 solid}")
	w("html,:host{line-height:1.5;-webkit-text-size-adjust:100%;tab-size:4;font-family:" + fontOrDefault(theme, "sans") + ";font-feature-settings:normal;font-variation-settings:normal;-webkit-tap-highlight-color:transparent}")
	w("hr{height:0;color:inherit;border-top-width:1px}")
	w("abbr:where([title]){-webkit-text-decoration:underline dotted;text-decoration:underline dotted}")
	w("h1,h2,h3,h4,h5,h6{font-size:inherit;font-weight:inherit}")
	w("a{color:inherit;-webkit-text-decoration:inherit;text-decoration:inherit}")
	w("b,strong{font-weight:bolder}")
	w("code,kbd,samp,pre{font-family:" + fontOrDefault(theme, "mono") + ";font-feature-settings:normal;font-variation-settings:normal;font-size:1em}")
	w("small{font-size:80%}")
	w("sub,sup{font-size:75%;line-height:0;position:relative;vertical-align:baseline}")
	w("sub{bottom:-0.25em}")
	w("sup{top:-0.5em}")
	w("table{text-indent:0;border-color:inherit;border-collapse:collapse}")
	w(":-moz-focusring:where(:not(iframe)){outline:auto}")
	w("progress{vertical-align:baseline}")
	w("summary{display:list-item}")
	w("ol,ul,menu{list-style:none}")
	w("img,svg,video,canvas,audio,iframe,embed,object{display:block;vertical-align:middle}")
	w("img,video{max-width:100%;height:auto}")
	w("button,input,select,optgroup,textarea,::file-selector-button{font:inherit;font-feature-settings:inherit;font-variation-settings:inherit;letter-spacing:inherit;color:inherit;border-radius:0;background-color:transparent;opacity:1}")
	w(":where(select:is([multiple],[size])) optgroup{font-weight:bolder}")
	w(":where(select:is([multiple],[size])) optgroup option{padding-inline-start:20px}")
	w("::file-selector-button{margin-inline-end:4px}")
	w("::placeholder{opacity:1}")
	w("@supports (not (-webkit-appearance:-apple-pay-button)) or (contain-intrinsic-size:1px){::placeholder{color:color-mix(in oklab,currentcolor 50%,transparent)}}")
	w("textarea{resize:vertical}")
	w("::-webkit-search-decoration{-webkit-appearance:none}")
	w("::-webkit-date-and-time-value{min-height:1lh;text-align:inherit}")
	w("::-webkit-datetime-edit{display:inline-flex}")
	w("::-webkit-datetime-edit-fields-wrapper{padding:0}")
	w("::-webkit-datetime-edit,::-webkit-datetime-edit-year-field,::-webkit-datetime-edit-month-field,::-webkit-datetime-edit-day-field,::-webkit-datetime-edit-hour-field,::-webkit-datetime-edit-minute-field,::-webkit-datetime-edit-second-field,::-webkit-datetime-edit-millisecond-field,::-webkit-datetime-edit-meridiem-field{padding-block:0}")
	w("::-webkit-calendar-picker-indicator{line-height:1}")
	w(":-moz-ui-invalid{box-shadow:none}")
	w("button,input:where([type='button'],[type='reset'],[type='submit']),::file-selector-button{appearance:button}")
	w("::-webkit-inner-spin-button,::-webkit-outer-spin-button{height:auto}")
	w("[hidden]:where(:not([hidden='until-found'])){display:none !important}")

	return b.String()
}

// fontOrDefault returns the configured font stack for a family key, falling back
// to Tailwind's default.
func fontOrDefault(theme TailwindTheme, key string) string {
	if v, ok := theme.FontFamily[key]; ok && v != "" {
		return theme.themeRef("font-"+key, v)
	}
	if d, ok := defaultFontFamily()[key]; ok {
		return d
	}
	return "sans-serif"
}
