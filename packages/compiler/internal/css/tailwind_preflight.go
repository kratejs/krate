package css

import "strings"

// TailwindPreflight returns Tailwind's Preflight base reset (opt-in via
// `tailwind.preflight`). It normalizes box-sizing, borders, margins, headings,
// lists, media, and form controls, and sets the default transform/space custom
// properties so composed utilities behave correctly.
func TailwindPreflight(theme TailwindTheme) string {
	var b strings.Builder

	// Box sizing + border defaults.
	b.WriteString("*,::before,::after{box-sizing:border-box;border-width:0;border-style:solid;border-color:currentColor}\n")
	b.WriteString("::before,::after{--tw-content:''}\n")
	b.WriteString("html,:host{line-height:1.5;-webkit-text-size-adjust:100%;tab-size:4;font-family:" + fontOrDefault(theme, "sans") + "}\n")
	b.WriteString("body{line-height:inherit;margin:0}\n")

	// Headings + text.
	b.WriteString("h1,h2,h3,h4,h5,h6{font-size:inherit;font-weight:inherit}\n")
	b.WriteString("a{color:inherit;text-decoration:inherit}\n")
	b.WriteString("b,strong{font-weight:bolder}\n")
	b.WriteString("small{font-size:80%}\n")
	b.WriteString("sub,sup{font-size:75%;line-height:0;position:relative;vertical-align:baseline}\n")
	b.WriteString("code,kbd,samp,pre{font-family:" + fontOrDefault(theme, "mono") + ";font-size:1em}\n")

	// Lists.
	b.WriteString("ol,ul,menu{list-style:none;margin:0;padding:0}\n")
	b.WriteString("blockquote,dl,dd,h1,h2,h3,h4,h5,h6,hr,figure,p,pre{margin:0}\n")

	// Media + tables.
	b.WriteString("img,svg,video,canvas,audio,iframe,embed,object{display:block;vertical-align:middle}\n")
	b.WriteString("img,video{max-width:100%;height:auto}\n")
	b.WriteString("table{border-collapse:collapse;border-color:inherit;text-indent:0}\n")
	b.WriteString("hr{height:0;color:inherit;border-top-width:1px}\n")

	// Forms.
	b.WriteString("button,input,optgroup,select,textarea{font:inherit;color:inherit;margin:0;padding:0;background:transparent;line-height:inherit}\n")
	b.WriteString("button,[type='button'],[type='reset'],[type='submit']{-webkit-appearance:button;background-color:transparent;background-image:none;cursor:pointer}\n")
	b.WriteString(":disabled{cursor:default}\n")
	b.WriteString("textarea{resize:vertical}\n")
	b.WriteString("::placeholder{opacity:1;color:#9ca3af}\n")

	// Transform + space composition defaults.
	b.WriteString("*,::before,::after{" +
		"--tw-border-spacing-x:0;--tw-border-spacing-y:0;" +
		"--tw-translate-x:0;--tw-translate-y:0;--tw-translate-z:0;" +
		"--tw-rotate:0;--tw-rotate-x:0;--tw-rotate-y:0;--tw-rotate-z:0;" +
		"--tw-skew-x:0;--tw-skew-y:0;" +
		"--tw-scale-x:1;--tw-scale-y:1;--tw-scale-z:1;" +
		"--tw-space-x-reverse:0;--tw-space-y-reverse:0;" +
		"--tw-divide-x-reverse:0;--tw-divide-y-reverse:0;" +
		"--tw-shadow:0 0 #0000;--tw-ring-color:rgb(59 130 246 / 0.5);" +
		"--tw-ring-offset-width:0px;--tw-ring-offset-color:#fff;--tw-ring-inset:;" +
		"}\n")

	return b.String()
}

// fontOrDefault returns the configured font stack for a family key, falling back
// to Tailwind's default.
func fontOrDefault(theme TailwindTheme, key string) string {
	if v, ok := theme.FontFamily[key]; ok && v != "" {
		return v
	}
	if d, ok := defaultFontFamily()[key]; ok {
		return d
	}
	return "sans-serif"
}
