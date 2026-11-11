package pkg

import (
	"io/fs"
	"testing"
)

func TestUnixModeFor(t *testing.T) {
	tests := []struct {
		name       string
		unreliable bool
		mode       fs.FileMode
		head       string
		ext        string
		want       fs.FileMode
	}{
		{"unix keeps mode", false, 0o640, "\x7fELF", "", 0o640},
		{"elf", true, 0o666, "\x7fELF", "", 0o755},
		{"macho 64", true, 0o666, "\xcf\xfa\xed\xfe", "", 0o755},
		{"macho fat", true, 0o666, "\xca\xfe\xba\xbe", "", 0o755},
		{"shebang", true, 0o666, "#!/b", "", 0o755},
		{"script by extension", true, 0o666, "echo", ".sh", 0o755},
		{"pe", true, 0o666, "MZ\x90\x00", ".exe", 0o755},
		{"data", true, 0o666, "{\"a\"", ".json", 0o644},
		{"empty", true, 0o666, "", "", 0o644},
	}
	for _, tc := range tests {
		if got := unixModeFor(tc.unreliable, tc.mode, []byte(tc.head), tc.ext); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
