package build

import (
	"os"
	"path/filepath"
	"testing"
)

// When the Go host binary cannot be replaced (Delve holds it open on
// Windows), the dev loop names the new build krate-gohost-N; the manifest
// points at it and older numbered builds are removed.
func TestGoHostBinaryNamesAndStaleRemoval(t *testing.T) {
	dir := t.TempDir()
	nd := &nativeDev{appDir: dir, goos: "windows"}
	if got := nd.goHostBinary(); got != "gohost/krate-gohost.exe" {
		t.Fatalf("default = %q", got)
	}
	nd.goos = "linux"
	if got := nd.goHostBinary(); got != "gohost/krate-gohost" {
		t.Fatalf("default = %q", got)
	}
	gh := filepath.Join(dir, "gohost")
	if err := os.MkdirAll(gh, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"krate-gohost", "krate-gohost-1", "krate-gohost-2", "krate-gohost-3.next"} {
		if err := os.WriteFile(filepath.Join(gh, n), nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	nd.goBin = "gohost/krate-gohost-2"
	if got := nd.goHostBinary(); got != "gohost/krate-gohost-2" {
		t.Fatalf("current = %q", got)
	}
	nd.removeStaleGoHosts()
	for n, want := range map[string]bool{"krate-gohost": true, "krate-gohost-1": false, "krate-gohost-2": true, "krate-gohost-3.next": true} {
		_, err := os.Stat(filepath.Join(gh, n))
		if (err == nil) != want {
			t.Errorf("%s present = %v, want %v", n, err == nil, want)
		}
	}
}
