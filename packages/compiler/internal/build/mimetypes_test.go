package build

import (
	"mime"
	"testing"
)

// TestWebContentTypesArePinned guards against the OS table: on Windows the
// registry gave .js without a charset and nothing for .woff2.
func TestWebContentTypesArePinned(t *testing.T) {
	t.Parallel()
	for ext, want := range map[string]string{
		".js":    "text/javascript; charset=utf-8",
		".woff2": "font/woff2",
		".otf":   "font/otf",
		".ico":   "image/x-icon",
		".map":   "application/json",
	} {
		if got := mime.TypeByExtension(ext); got != want {
			t.Errorf("%s = %q, want %q", ext, got, want)
		}
	}
}
