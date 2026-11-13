package hosttransport

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestShortSocketDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("socket paths only: Windows host children use loopback TCP, and HOME is not the home directory there")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	d := ShortSocketDir("sdk.sock", 4096)
	if d == "" || !strings.HasPrefix(d, home) {
		t.Fatalf("dir = %q", d)
	}
	if st, err := os.Stat(d); err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("dir mode: %v %v", st, err)
	}
	if got := ShortSocketDir("sdk.sock", 10); got != "" {
		t.Errorf("a dir that cannot fit must be refused, got %q", got)
	}
}
