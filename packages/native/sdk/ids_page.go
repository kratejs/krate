package sdk

// Ids of the web contents areas: page (print, find, zoom, capture),
// downloads and cookies.
func init() {
	IDs = append(IDs, []ID{
		{"krate/page.print", "page", "[view?: string]", "void", "page"},
		{"krate/page.printToPdf", "page", "[view: string, options?: PdfOptions]", "Uint8Array", "page"},
		{"krate/page.find", "page", "[view: string, text: string, options?: FindOptions]", "void", "page"},
		{"krate/page.stopFind", "page", "[view: string]", "void", "page"},
		{"krate/page.findEvents", "page", "[] (stream)", "FindEvent", "page"},
		{"krate/page.zoom", "page", "[view: string]", "number", "page"},
		{"krate/page.setZoom", "page", "[view: string, factor: number]", "void", "page"},
		{"krate/page.capture", "page", "[view: string]", "Uint8Array", "page"},

		{"krate/downloads.configure", "downloads", "[options: DownloadOptions]", "void", "downloads"},
		{"krate/downloads.list", "downloads", "[]", "DownloadInfo[]", "downloads"},
		{"krate/downloads.cancel", "downloads", "[id: number]", "void", "downloads"},
		{"krate/downloads.events", "downloads", "[] (stream)", "DownloadEvent", "downloads"},

		{"krate/cookies.list", "cookies", "[url?: string]", "Cookie[]", "cookies"},
		{"krate/cookies.get", "cookies", "[name: string, url?: string]", "Cookie | null", "cookies"},
		{"krate/cookies.set", "cookies", "[cookie: CookieInput, url?: string]", "void", "cookies"},
		{"krate/cookies.delete", "cookies", "[name: string, url?: string]", "void", "cookies"},
		{"krate/cookies.clearSiteData", "cookies", "[options?: ClearOptions]", "void", "cookies"},
	}...)
}
