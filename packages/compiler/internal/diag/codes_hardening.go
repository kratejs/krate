package diag

// KR835x: build hardening checks that turn a silent mistake into an error.
const (
	// CodeUnsafeOutDir: an outDir that is, or contains, the project's sources.
	CodeUnsafeOutDir Code = "KR8350"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeUnsafeOutDir,
			Title: "outDir would delete project sources",
			Explanation: "The build empties outDir before it writes the site, and `krate clean` deletes it. An outDir " +
				"that is, or contains, the project root, the pages directory or the public directory (`outDir: \"..\"`, " +
				"`\".\"`, `\"src\"`) would delete the project's own files, so the build stops before anything is " +
				"removed. Point outDir at a dedicated directory such as `dist`; a directory outside the project that " +
				"holds none of your sources, such as `../site-dist`, is fine. Setting KRATE_ALLOW_UNSAFE_OUTDIR=1 " +
				"skips this check.",
		},
	)
}
