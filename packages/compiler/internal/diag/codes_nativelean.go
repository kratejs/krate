package diag

// Native (desktop) target: lean-page checks, memory hygiene for UI code
// (KR7270 to KR7275). All are warnings.
const (
	// CodeLeanImageOversize: an image file is far larger than its rendered size.
	CodeLeanImageOversize Code = "KR7270"
	// CodeLeanDataURI: a large base64 data: URL is inlined into code or CSS.
	CodeLeanDataURI Code = "KR7271"
	// CodeLeanWebFont: a web font is loaded where a system font stack would do.
	CodeLeanWebFont Code = "KR7272"
	// CodeLeanCompositing: a CSS effect forces extra compositing layers.
	CodeLeanCompositing Code = "KR7273"
	// CodeLeanLongList: a long or unbounded list is rendered without windowing.
	CodeLeanLongList Code = "KR7274"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeLeanImageOversize,
			Title: "Image much larger than its rendered size",
			Explanation: "An `<img>` has literal `width`/`height` (or an inline style size) that is several times " +
				"smaller than the image file's pixel size. The webview decodes the whole bitmap and keeps it in " +
				"memory, so a 2000 px photo shown at 100 px costs about 16 MB of decoded pixels for a 100 px " +
				"picture. Resize the file to at most about twice its rendered size (the factor is " +
				"`native.lean.imageRatio`, default 2). The check only runs when the file is a local PNG, JPEG, " +
				"GIF or WebP and the rendered size is a literal.",
		},
		CodeInfo{
			Code:  CodeLeanDataURI,
			Title: "Large data: URL inlined into code or CSS",
			Explanation: "A base64 `data:` URL above the threshold (`native.lean.dataUriKb`, default 16 KiB) sits " +
				"in a source or stylesheet file. It is parsed with the page, kept as a string, and decoded again " +
				"for each use, so the bytes cost memory more than once. Put the asset in the public directory and " +
				"reference it by URL; the shell serves it from the app bundle and the webview decodes it once.",
		},
		CodeInfo{
			Code:  CodeLeanWebFont,
			Title: "Web font where a system font stack would do",
			Explanation: "The project loads a web font (an `@font-face` rule with a font file, a Google Fonts or " +
				"other font-service link, or an `@fontsource` import). Each family and weight costs the file, a " +
				"parsed copy and shaped glyph caches in the web process. Icon fonts are not reported. This is a " +
				"heuristic, not a measurement: it cannot know whether the design needs the typeface. Prefer " +
				"`system-ui, -apple-system, \"Segoe UI\", sans-serif`, and keep a web font only when the brand needs it " +
				"(silence this with `native.lean.ignore: [\"KR7272\"]`).",
		},
		CodeInfo{
			Code:  CodeLeanCompositing,
			Title: "CSS effect that forces compositing layers",
			Explanation: "`backdrop-filter`, a large `filter: blur()` (16 px or more) or a blur in an animated " +
				"rule, a very large `box-shadow` on a tag or universal selector, and `will-change` on many " +
				"elements each give the element its own GPU layer with its own backing store, and a " +
				"backdrop-filter also needs the content behind it re-rendered. Layers are memory in the GPU " +
				"process. Use them on a few elements at most, drop them from lists and repeated components, or " +
				"use a solid or semi-transparent background instead.",
		},
		CodeInfo{
			Code:  CodeLeanLongList,
			Title: "Long list rendered without windowing",
			Explanation: "A `.map()` renders a JSX element per item over an array with more than " +
				"`native.lean.listItems` entries (default 100) known at build time, or over a value from host code, " +
				"whose length the UI does not bound, and the file uses no windowing component or `.slice()`. " +
				"Every item stays in the DOM, so memory grows with the list. Render only the visible window " +
				"(a virtual list component), page the data with `.slice()`, or have host code return one page at a " +
				"time.",
		},
	)
}
