package diag

// KR88xx: images and other build-time media.
const (
	// CodeImageSource: an <Image> src that is missing, unreadable or not a supported image.
	CodeImageSource Code = "KR8801"
	// CodeImageRemote: an <Image> remote src that images.remotePatterns does not allow.
	CodeImageRemote Code = "KR8802"
	// CodeImageDynamicProp: an <Image> prop that must be known at build time but is not.
	CodeImageDynamicProp Code = "KR8803"
	// CodeImageAlt: an <Image> without alt text.
	CodeImageAlt Code = "KR8804"
	// CodeIconUnresolved: an <Icon> whose icon is not in icons/ and cannot be fetched.
	CodeIconUnresolved Code = "KR8840"
	// CodeCollectionShipped: a content collection copied into a page script.
	CodeCollectionShipped Code = "KR8810"
	// CodeScopeDynamicOption: a part of a zero-JS component (Tabs, Accordion, ...)
	// whose value is not a literal, so its option cannot be generated.
	CodeScopeDynamicOption Code = "KR8820"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeImageSource,
			Title: "Image source cannot be used",
			Explanation: "An `<Image>` is optimised while the site is built, so its src must name an image file that exists " +
				"(a path starting with / is relative to the public directory) and can be decoded. Krate reports the " +
				"problem as a build error instead of shipping a broken image. Fix the path or the file.",
		},
		CodeInfo{
			Code:  CodeImageRemote,
			Title: "Remote image host is not allowed",
			Explanation: "A remote `<Image src=\"https://...\">` is only passed through when its host matches " +
				"images.remotePatterns in krate.config.ts. Add the host to the allow list, or download the file into " +
				"the public directory.",
		},
		CodeInfo{
			Code:  CodeImageDynamicProp,
			Title: "Image prop must be known at build time",
			Explanation: "An `<Image>` generates its variants during the build, so src, width, height, quality and priority " +
				"must be literals (or consts initialised with literals) by the time the page is rendered. A value " +
				"that only exists in the browser cannot be used. alt, sizes and className may be expressions.",
		},
		CodeInfo{
			Code:  CodeImageAlt,
			Title: "Image has no alt text",
			Explanation: "An `<Image>` without an alt attribute is announced by screen readers by its file name. Add a " +
				"description, or alt=\"\" when the image is purely decorative. This is a warning.",
		},
		CodeInfo{
			Code:  CodeIconUnresolved,
			Title: "Icon cannot be resolved",
			Explanation: "An `<Icon name=\"...\">` is compiled to inline SVG while the site is built. A name without a " +
				"colon is a local icon and must exist as `icons/<name>.svg` in the project; `set:name` (for example " +
				"`lucide:home`) is fetched from the Iconify API and cached under `.krate/cache/icons`. When neither " +
				"works the build fails and names the file or lookup that failed, instead of putting an error message in " +
				"the page. Fix the name, add the file, or pre-populate the cache for offline builds. This is an error.",
		},
		CodeInfo{
			Code:  CodeCollectionShipped,
			Title: "Content collection is shipped to the browser",
			Explanation: "A `getCollection(\"name\")` chain is normally evaluated while the site is built and only its " +
				"result is rendered. When the chain depends on a value that only exists in the browser (a signal, an " +
				"event), the collection has to be evaluated there, so its entries are copied into the page script. " +
				"Krate ships only the fields the page mentions (the body and rendered html only when referenced), but " +
				"every entry in the collection, drafts included, is still included. Filter with build-time values, or " +
				"compute the list on the server. This is a warning.",
		},
		CodeInfo{
			Code:  CodeScopeDynamicOption,
			Title: "Component option value must be a literal",
			Explanation: "Tabs, Accordion and the other zero-JS components turn each part's value into CSS while the " +
				"site is built, so every `value` on their parts must be a string literal in the source. A value " +
				"read from data (for example `value={item.id}` inside `items.map(...)`) cannot be enumerated: the " +
				"panels for that value get no show/hide rules, defaultValue is ignored and every panel stays " +
				"visible. Write the values out, or use `<Select>`/a `createSignal` based component for data-driven " +
				"options. This is a warning.",
		},
	)
}
