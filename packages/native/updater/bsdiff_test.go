package updater

import (
	"bytes"
	"math/rand"
	"sort"
	"testing"
)

func TestSuffixSortMatchesNaive(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for _, alpha := range []int{2, 4, 256} {
		for _, n := range []int{1, 2, 5, 17, 100, 1000, 5000} {
			b := make([]byte, n)
			for i := range b {
				b[i] = byte(rng.Intn(alpha))
			}
			I := bsSuffixSort(b)
			want := make([]int, n+1)
			for i := range want {
				want[i] = i
			}
			sort.Slice(want, func(x, y int) bool { return bytes.Compare(b[want[x]:], b[want[y]:]) < 0 })
			for i := range want {
				if int(I[i]) != want[i] {
					t.Fatalf("alpha %d n %d: I[%d]=%d want %d", alpha, n, i, I[i], want[i])
				}
			}
		}
	}
}

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

func TestBspatchRejectsGarbage(t *testing.T) {
	old := bytes.Repeat([]byte("abc"), 100)
	blob, _ := diffBlob(old, append(old, 'x'))
	for i := 0; i < len(blob); i += 3 {
		bad := append([]byte(nil), blob...)
		bad[i] ^= 0xff
		// Must not panic; a wrong result is caught by the caller's hash.
		_, _ = applyDiffBlob(old, bad, len(old)+1)
	}
	if _, err := applyDiffBlob(old, blob[:len(blob)/2], len(old)+1); err == nil {
		t.Error("truncated blob accepted")
	}
	if _, err := applyDiffBlob(old, []byte("not deflate"), 10); err == nil {
		t.Error("garbage accepted")
	}
}
