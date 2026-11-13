package css

import (
	"testing"
)

func TestEscapeSelector(t *testing.T) {
	tests := map[string]string{
		"w-1/2":         "w-1\\/2",
		"w-[100px]":     "w-\\[100px\\]",
		"bg-red-500/50": "bg-red-500\\/50",
		"text-[#fff]":   "text-\\[\\#fff\\]",
		"p-1.5":         "p-1\\.5",
		"hover:bg":      "hover\\:bg",
		"2xl:flex":      "\\32 xl\\:flex",
		"-mt-4":         "-mt-4",
	}
	for in, want := range tests {
		if got := EscapeClass(in); got != want {
			t.Errorf("EscapeClass(%q) = %q, want %q", in, got, want)
		}
	}
}
