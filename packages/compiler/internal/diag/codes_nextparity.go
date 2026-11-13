package diag

// Next.js-parity features: access-denied pages, metadata and shared caches.
const (
	// CodeStaticAccessDenied: forbidden() or unauthorized() in a statically
	// built page or in a layout, where the call can never run per request.
	CodeStaticAccessDenied Code = "KR8604"

	// CodeMetadataRoute: a sitemap, robots, manifest or image route file failed to build.
	CodeMetadataRoute Code = "KR7130"
	// CodeImageUnsupported: ImageResponse used something the renderer cannot draw.
	CodeImageUnsupported Code = "KR7131"
	// CodeImageRouteParams: an image route in a dynamic segment cannot be built.
	CodeImageRouteParams Code = "KR7132"
	// CodeMetadataRouteOverride: a metadata file replaced a plugin's or config's output.
	CodeMetadataRouteOverride Code = "KR7133"
	// CodeMetadataRouteDynamic: dynamic or revalidate on a metadata route that
	// cannot run per request.
	CodeMetadataRouteDynamic Code = "KR7134"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeMetadataRoute,
			Title: "Metadata route could not be built",
			Explanation: "A `sitemap.ts`, `robots.ts`, `manifest.ts`, `opengraph-image` or `twitter-image` file under the pages " +
				"directory runs once at build time and its result becomes `sitemap.xml`, `robots.txt`, " +
				"`manifest.webmanifest` or a PNG. The file threw, returned a value of the wrong shape (a sitemap " +
				"entry without a `url`, a robots file without `rules`), or could not be loaded. Fix the error " +
				"named in the message. This is an error: the file would otherwise silently produce nothing.",
		},
		CodeInfo{
			Code:  CodeImageUnsupported,
			Title: "ImageResponse uses something the renderer cannot draw",
			Explanation: "ImageResponse draws a documented JSX subset with a built-in renderer (no headless browser): " +
				"flexbox layout, text, colours, gradients, borders, box shadows, opacity, overflow hidden and images. " +
				"Anything else (an element such as `<svg>`, a style such as `transform` or `filter`, a unit such as " +
				"`vh`, a character the font has no glyph for) is listed here instead of being drawn wrong. Use the " +
				"supported subset, or pass a font with `options.fonts` for other characters. This is an error.",
		},
		CodeInfo{
			Code:  CodeImageRouteParams,
			Title: "Image route in a dynamic segment has no params to build with",
			Explanation: "An `opengraph-image` or `twitter-image` file inside a dynamic segment (`blog/[slug]/`) is built " +
				"once per entry of the segment page's `generateStaticParams`. This one has no matching page or no " +
				"`generateStaticParams`, so no image was built and no `og:image` tag is emitted for it. Export " +
				"`generateStaticParams` from the segment's page. This is a warning.",
		},
		CodeInfo{
			Code:  CodeMetadataRouteOverride,
			Title: "A metadata file replaces a plugin or config output",
			Explanation: "`sitemap.ts` or `robots.ts` defines the file, so the sitemap or robots plugin (and the `robots` " +
				"and `seo` config that feed it) did not write its own. The metadata file wins. Remove one of them to " +
				"silence this warning. This is a warning.",
		},
		CodeInfo{
			Code:  CodeMetadataRouteDynamic,
			Title: "dynamic or revalidate on a metadata route that cannot run per request",
			Explanation: "A `sitemap.ts`, `robots.ts` or `manifest.ts` that exports `dynamic = \"force-dynamic\"` or a " +
				"`revalidate` runs per request on the server. This one sits in a dynamic segment (`blog/[slug]/`), where " +
				"a per-request route has no URL to answer, so it still runs once at build time and the export is ignored. " +
				"Move the file to a static directory or remove the export. This is a warning.",
		},
		CodeInfo{
			Code:  CodeStaticAccessDenied,
			Title: "forbidden() or unauthorized() never runs per request",
			Explanation: "forbidden() and unauthorized() answer one request with a 403 or 401 page, so they only work in code " +
				"that runs on every request: a page rendered with ssr, isr or streaming, an async server component, a " +
				"server action or an API route. This call sits in a statically built page, or in a layout (layouts are " +
				"baked into the shell at build time), so it never runs and the page is served to everyone. Make the page " +
				"request-time (`export const dynamic = \"force-dynamic\"`), or call the function from an async server " +
				"component that the page renders.",
		},
	)
}
