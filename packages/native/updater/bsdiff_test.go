package updater

import (
	"bytes"
	"math/rand"
	"testing"
)

func mutate(rng *rand.Rand, b []byte) []byte {
	out := append([]byte(nil), b...)
	for i := 0; i < len(b)/50+3; i++ {
		out[rng.Intn(len(out))] = byte(rng.Intn(256))
	}
	// insert and delete regions
	at := rng.Intn(len(out))
	out = append(out[:at], append(bytes.Repeat([]byte("INSERTED"), 20), out[at:]...)...)
	at = rng.Intn(len(out) - 100)
	out = append(out[:at], out[at+80:]...)
	return out
}

func TestBsdiffRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	base := make([]byte, 200000)
	for i := range base {
		base[i] = byte(rng.Intn(40))
	}
	cases := map[string][2][]byte{
		"mutated":   {base, mutate(rng, base)},
		"empty old": {nil, []byte("hello world")},
		"empty new": {base[:100], nil},
		"unrelated": {base[:5000], bytes.Repeat([]byte{9, 8, 7}, 3000)},
		"identical": {base, base},
		"tiny":      {[]byte("a"), []byte("b")},
	}
	for name, c := range cases {
		blob, err := diffBlob(c[0], c[1])
		if err != nil {
			t.Fatal(name, err)
		}
		got, err := applyDiffBlob(c[0], blob, len(c[1]))
		if err != nil {
			t.Fatal(name, err)
		}
		if !bytes.Equal(got, c[1]) {
			t.Fatalf("%s: patched output differs", name)
		}
		if name == "mutated" && len(blob) > len(c[1])/10 {
			t.Errorf("mutated: patch is %d bytes for a %d byte file", len(blob), len(c[1]))
		}
	}
}
