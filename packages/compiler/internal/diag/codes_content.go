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
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeImageSource,
			Title: "Image source cannot be used",
			Explanation: "An <Image> is optimised while the site is built, so its src must name an image file that exists " +
				"(a path starting with / is relative to the public directory) and can be decoded. Krate reports the " +
				"problem as a build error instead of shipping a broken image. Fix the path or the file.",
		},
		CodeInfo{
			Code:  CodeImageRemote,
			Title: "Remote image host is not allowed",
			Explanation: "A remote <Image src=\"https://...\"> is only passed through when its host matches " +
				"images.remotePatterns in krate.config.ts. Add the host to the allow list, or download the file into " +
				"the public directory.",
		},
		CodeInfo{
			Code:  CodeImageDynamicProp,
			Title: "Image prop must be known at build time",
			Explanation: "<Image> generates its variants during the build, so src, width, height, quality and priority " +
				"must be literals (or consts initialised with literals) by the time the page is rendered. A value " +
				"that only exists in the browser cannot be used. alt, sizes and className may be expressions.",
		},
		CodeInfo{
			Code:  CodeImageAlt,
			Title: "Image has no alt text",
			Explanation: "An <Image> without an alt attribute is announced by screen readers by its file name. Add a " +
				"description, or alt=\"\" when the image is purely decorative. This is a warning.",
		},
	)
}
