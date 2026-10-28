package goext

import (
	"os"
	"testing"
)

// TestEmbeddedSourcesInSync guards against drift: the vendored copies the
// compiler writes into a project (.krate/goext) must match the real packages.
func TestEmbeddedSourcesInSync(t *testing.T) {
	cases := []struct {
		path string
		data []byte
	}{
		{"krate/krate.go", KrateSrc},
		{"krate/request.go", KrateRequestSrc},
		{"krate/revalidate.go", KrateRevalidateSrc},
		{"runtime/runtime.go", RuntimeSrc},
	}
	for _, c := range cases {
		onDisk, err := os.ReadFile(c.path)
		if err != nil {
			t.Fatalf("reading %s: %v", c.path, err)
		}
		if string(onDisk) != string(c.data) {
			t.Errorf("%s is out of sync with its embedded copy", c.path)
		}
	}
}
