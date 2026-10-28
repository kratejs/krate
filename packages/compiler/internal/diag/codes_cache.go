package diag

// KR86xx: data cache, revalidation and page segment config.
const (
	// CodeSegmentExportValue: an unsupported `export const revalidate` or
	// `export const dynamic` value.
	CodeSegmentExportValue Code = "KR8601"
	// CodeStaticDataCache: a static page reads tagged, timed or no-store data.
	CodeStaticDataCache Code = "KR8602"
	// CodeRevalidateStaticTarget: revalidatePath names a static (SSG) route.
	CodeRevalidateStaticTarget Code = "KR8603"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeSegmentExportValue,
			Title: "Unsupported page segment export value",
			Explanation: "Krate reads the Next.js-style page exports `export const revalidate` (a constant number of " +
				"seconds, 0 for per-request rendering, or false to cache indefinitely) and `export const dynamic` " +
				"(\"auto\", \"force-dynamic\", \"force-static\" or \"error\") at build time. The value could not be read " +
				"statically or is not one of the supported values, so it is ignored. Write a literal value.",
		},
		CodeInfo{
			Code:  CodeStaticDataCache,
			Title: "Static page reads revalidating data",
			Explanation: "The page reads data with cache tags, a revalidate window or `cache: \"no-store\"`, but it is " +
				"rendered statically (it is pinned with `dynamic = \"force-static\"`, `revalidate = false`, or the " +
				"project uses `output: \"static\"`). Static HTML is baked once at build time: revalidateTag, " +
				"revalidatePath and revalidate windows cannot refresh it. Remove the static pin or export " +
				"`revalidate = N` so the page is served with ISR.",
		},
		CodeInfo{
			Code:  CodeRevalidateStaticTarget,
			Title: "revalidatePath targets a static page",
			Explanation: "A revalidatePath() call names a route that is rendered statically (SSG). The page's HTML is " +
				"a file baked at build time, so revalidation cannot rebuild it: at runtime the call reports the route " +
				"under `skipped` and logs a warning. Make the target page ISR (`export const revalidate = N`) or SSR " +
				"(`export const dynamic = \"force-dynamic\"`), or rebuild the site to refresh it.",
		},
	)
}
