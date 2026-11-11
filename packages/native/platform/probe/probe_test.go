package probe

import "testing"

func TestAtLeast(t *testing.T) {
	cases := []struct {
		v, min string
		want   bool
	}{
		{"2.40.0", "2.40", true},
		{"2.39.9", "2.40", false},
		{"2.54.0", "2.40", true},
		{"3.0.0", "2.40", true},
		{"2.40", "2.40.1", false},
		{"1.0.2903.40", "", true},
	}
	for _, c := range cases {
		if got := AtLeast(c.v, c.min); got != c.want {
			t.Errorf("AtLeast(%q, %q) = %v, want %v", c.v, c.min, got, c.want)
		}
	}
}
