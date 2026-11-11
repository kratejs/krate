package wire

import (
	"math"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Forms that only one side emits. The shared vectors in testdata hold the
// canonical forms; this file holds what the TS codec (JSON.stringify) may send
// and what Go emits that JS does not.

func TestDecodeJSNumbers(t *testing.T) {
	floats := map[string]float64{
		"1e+21": 1e21, "5e-324": 5e-324, "1e-7": 1e-7, "1.7976931348623157e+308": math.MaxFloat64,
		"123456789012345680000": 123456789012345680000, "1E5": 1e5, "1e5": 1e5, "-1.5E-3": -1.5e-3,
		"0.000001": 1e-6, "0": 0, "-0": math.Copysign(0, -1), "9007199254740991": 9007199254740991,
	}
	for tok, want := range floats {
		d := dec(t, `[`+tok+`]`)
		if err := d.BeginArray(); err != nil {
			t.Fatal(err)
		}
		got, err := d.Float()
		if err != nil || got != want || math.Signbit(got) != math.Signbit(want) {
			t.Errorf("%s: got %v, %v want %v", tok, got, err, want)
		}
		if err := d.EndArray(); err != nil {
			t.Errorf("%s: %v", tok, err)
		}
	}
	d := dec(t, `[9007199254740991,-9223372036854775808,18446744073709551615]`)
	if err := d.BeginArray(); err != nil {
		t.Fatal(err)
	}
	if v, err := d.Int(); err != nil || v != 9007199254740991 {
		t.Errorf("int: %d %v", v, err)
	}
	if v, err := d.Int(); err != nil || v != math.MinInt64 {
		t.Errorf("min: %d %v", v, err)
	}
	if v, err := d.Uint(); err != nil || v != math.MaxUint64 {
		t.Errorf("max: %d %v", v, err)
	}
	// JS never sends these; they are not JSON numbers.
	for _, bad := range []string{"01", "+1", ".5", "1.", "1e", "NaN", "Infinity", "-"} {
		d := dec(t, `[`+bad+`]`)
		if err := d.BeginArray(); err != nil {
			t.Fatal(err)
		}
		_, err := d.Float()
		if err == nil {
			err = d.EndArray()
		}
		if err == nil {
			t.Errorf("%s: want error", bad)
		}
	}
}

func TestEncodeNumbersLikeJS(t *testing.T) {
	// Texts JSON.stringify produces for the same doubles.
	a, b := 0.1, 0.2
	sum := a + b
	cases := map[float64]string{
		1e21: "1e+21", 5e-324: "5e-324", 1e-7: "1e-7", 1.5e-7: "1.5e-7", 1e-6: "0.000001",
		123456789012345680000: "123456789012345680000", math.MaxFloat64: "1.7976931348623157e+308",
		sum: "0.30000000000000004", 1e20: "100000000000000000000", 100: "100",
	}
	for f, want := range cases {
		e := NewEncoder()
		e.Float(f)
		fr := e.Frame()
		if got := string(fr[4 : len(fr)-2]); got != want {
			t.Errorf("%v: got %s want %s", f, got, want)
		}
	}
}

func TestDecodeStringForms(t *testing.T) {
	cases := []struct{ js, want string }{
		{`"a\/b"`, "a/b"},
		{`"\u00E9\u00e9"`, "\u00e9\u00e9"},
		{"\"\u2028\u2029\"", "\u2028\u2029"},
		{`"\u2028\u2029"`, "\u2028\u2029"},
		{`"\ud83d\ude00"`, "\U0001F600"},
		{`"\ud800"`, "\ufffd"},
		{`"\udc00"`, "\ufffd"},
		{`"\ud800\u0041"`, "\ufffdA"},
		{`"\ud800x"`, "\ufffdx"},
		{`"\u0000\u001F\u007f"`, "\x00\x1f\x7f"},
		{"\"bad\xffbyte\"", "bad\ufffdbyte"},
	}
	for _, c := range cases {
		d := dec(t, c.js)
		got, err := d.String()
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v want %q", c.js, got, err, c.want)
		}
	}
	for _, bad := range []string{`"\x41"`, `"\u12"`, "\"a\nb\"", `"abc`} {
		if _, err := dec(t, bad).String(); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}

func TestEncodeStringsGoSpecific(t *testing.T) {
	e := NewEncoder()
	e.String("a\u2028b\u2029c\xffd")
	fr := e.Frame()
	if got, want := string(fr[4:len(fr)-2]), `"a\u2028b\u2029c\ufffdd"`; got != want {
		t.Errorf("got %s want %s", got, want)
	}
}

func TestDecodeTimeForms(t *testing.T) {
	want := time.Date(2026, 11, 11, 7, 23, 15, 123000000, time.UTC)
	for _, s := range []string{
		"2026-11-11T07:23:15.123Z", "2026-11-11T07:23:15.123000Z", "2026-11-11T08:23:15.123+01:00",
		"2026-11-11T02:23:15.123-05:00", "2026-11-11T07:23:15.123000000Z",
	} {
		d := dec(t, `{"$d":"`+s+`"}`)
		got, err := d.Time()
		if err != nil || !got.Equal(want) || got.Location() != time.UTC {
			t.Errorf("%s: got %v, %v", s, got, err)
		}
	}
	d := dec(t, `{"$d":"2026-11-11T07:23:15Z"}`)
	if got, err := d.Time(); err != nil || !got.Equal(want.Add(-123*time.Millisecond)) {
		t.Errorf("no fraction: %v %v", got, err)
	}
	for _, s := range []string{"2026-11-11", "2026-11-11 07:23:15Z", "2026-11-11T07:23:15", "x", "2026-13-11T07:23:15Z"} {
		if _, err := dec(t, `{"$d":"`+s+`"}`).Time(); err == nil {
			t.Errorf("%q: want error", s)
		}
	}
	e := NewEncoder()
	e.Time(time.Date(2026, 11, 11, 7, 23, 15, 999999999, time.UTC))
	e.BeginArray() // misuse: second root is an error
	if e.Err() == nil {
		t.Error("want error for second root")
	}
	e = NewEncoder()
	e.Time(time.Date(2026, 11, 11, 7, 23, 15, 5e6, time.UTC))
	fr := e.Frame()
	if got, want := string(fr[4:len(fr)-2]), `{"$d":"2026-11-11T07:23:15.005Z"}`; got != want {
		t.Errorf("got %s want %s", got, want)
	}
}

func TestEscapeAllTags(t *testing.T) {
	for _, tag := range []string{"$b", "$d", "$m", "$s"} {
		for _, key := range []string{tag, tag + "$"} {
			e := NewEncoder()
			e.BeginArray()
			e.BeginObject()
			e.Key(key)
			e.Int(1)
			e.EndObject()
			e.BeginObject()
			e.Key(key)
			e.Int(1)
			e.Key("other")
			e.Int(2)
			e.EndObject()
			e.BeginObject()
			e.Key("other")
			e.Int(2)
			e.Key(key)
			e.Int(1)
			e.EndObject()
			e.EndArray()
			fr := e.Frame()
			js := string(fr[4 : len(fr)-2])
			want := `[{"` + tag + `$":{"` + key + `":1}},{"` + key + `":1,"other":2},{"other":2,"` + key + `":1}]`
			if js != want {
				t.Fatalf("%s: got %s want %s", key, js, want)
			}
			d, err := NewDecoder(fr)
			if err != nil {
				t.Fatal(err)
			}
			if err := d.BeginArray(); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				if k := d.Kind(); k != KindObject {
					t.Fatalf("%s #%d: kind %v", key, i, k)
				}
				if err := d.BeginObject(); err != nil {
					t.Fatal(err)
				}
				for d.More() {
					k, _ := d.Key()
					if k != key && k != "other" {
						t.Fatalf("%s: key %q", key, k)
					}
					if err := d.Skip(); err != nil {
						t.Fatal(err)
					}
				}
				if err := d.EndObject(); err != nil {
					t.Fatal(err)
				}
			}
			if err := d.EndArray(); err != nil {
				t.Fatal(err)
			}
			if err := d.Finish(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestEscapedKeysAreNotReinterpreted(t *testing.T) {
	// The wrapped object's keys are literal; values still decode normally.
	d := dec(t, `{"$d$":{"$d":{"$d":"2026-11-11T07:23:15.123Z"}}}`)
	if err := d.BeginObject(); err != nil {
		t.Fatal(err)
	}
	k, _ := d.Key()
	if k != "$d" || d.Kind() != KindTime {
		t.Fatalf("key %q kind %v", k, d.Kind())
	}
	if _, err := d.Time(); err != nil {
		t.Fatal(err)
	}
	if d.More() {
		t.Fatal("more")
	}
	if err := d.EndObject(); err != nil {
		t.Fatal(err)
	}
	if err := d.Finish(); err != nil {
		t.Fatal(err)
	}
	// A wrapper that does not hold an object is malformed (TS rejects it too).
	for _, js := range []string{`{"$b$":1}`, `{"$s$":[]}`, `{"$m$":null}`, `{"$d$":"x"}`} {
		if err := dec(t, js).BeginObject(); err == nil {
			t.Errorf("%s: want error", js)
		}
	}
}

func TestKindOfTags(t *testing.T) {
	cases := map[string]Kind{
		`{"$b":0}`: KindBytes, `{"$d":"x"}`: KindTime, `{"$m":[]}`: KindMap, `{"$s":[]}`: KindSet,
		` { "$s" : [ 1 ] } `: KindSet, `{"\u0024s":[1]}`: KindSet,
		`{"$b":0,"x":1}`: KindObject, `{"x":1,"$b":0}`: KindObject, `{"$b$":{}}`: KindObject,
		`{"$m$":{}}`: KindObject, `{"$x":1}`: KindObject, `{}`: KindObject, `{"$s":[1,`: KindObject,
	}
	for js, want := range cases {
		d := &Decoder{src: []byte(js), maxDepth: 8}
		if got := d.Kind(); got != want {
			t.Errorf("%s: got %v want %v", js, got, want)
		}
		if d.pos != 0 || d.err != nil {
			t.Errorf("%s: Kind consumed input or failed", js)
		}
	}
}

func TestSetAndMapDecode(t *testing.T) {
	// $s reads as an array.
	for _, js := range []string{`{"$s":[1,2,3]}`, `[1,2,3]`} {
		d := dec(t, js)
		if err := d.BeginArray(); err != nil {
			t.Fatal(err)
		}
		var got []int64
		for d.More() {
			v, err := d.Int()
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, v)
		}
		if err := d.EndArray(); err != nil || !reflect.DeepEqual(got, []int64{1, 2, 3}) {
			t.Fatalf("%s: %v %v", js, got, err)
		}
		if err := d.Finish(); err != nil {
			t.Fatal(err)
		}
	}
	// BeginSet is strict.
	if err := dec(t, `[1]`).BeginSet(); err == nil {
		t.Error("BeginSet on array")
	}
	if err := dec(t, `{"$m":[]}`).BeginSet(); err == nil {
		t.Error("BeginSet on map")
	}
	if err := dec(t, `{"$s":[]}`).BeginMap(); err == nil {
		t.Error("BeginMap on set")
	}
	if err := dec(t, `{"$s":[]}`).BeginObject(); err == nil {
		t.Error("BeginObject on set")
	}
	if err := dec(t, `{"$b":0}`).BeginArray(); err == nil {
		t.Error("BeginArray on bytes")
	}

	// $m as an object when all keys are strings.
	d := dec(t, `{"$m":[["a",1],["b",[1,2]],["c",{"$s":[]}]],"x":1}`)
	if err := d.BeginObject(); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for d.More() {
		k, _ := d.Key()
		keys = append(keys, k)
		if err := d.Skip(); err != nil {
			t.Fatal(err)
		}
	}
	// The sibling "x" makes this a plain object, not a map.
	if !reflect.DeepEqual(keys, []string{"$m", "x"}) {
		t.Fatalf("multi-key object: %v", keys)
	}

	d = dec(t, `{"$m":[["a",1],["b",[1,2]],["c",{"$s":[3]}]]}`)
	if d.Kind() != KindMap {
		t.Fatal("kind")
	}
	if err := d.BeginObject(); err != nil {
		t.Fatal(err)
	}
	keys = nil
	for d.More() {
		k, err := d.Key()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k)
		if err := d.Skip(); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.EndObject(); err != nil || !reflect.DeepEqual(keys, []string{"a", "b", "c"}) {
		t.Fatalf("%v %v", keys, err)
	}
	if err := d.Finish(); err != nil {
		t.Fatal(err)
	}

	// An empty map is an empty object; a non-string key fails Key.
	d = dec(t, `{"$m":[]}`)
	if err := d.BeginObject(); err != nil {
		t.Fatal(err)
	}
	if d.More() {
		t.Error("empty map has members")
	}
	if err := d.EndObject(); err != nil {
		t.Error(err)
	}
	d = dec(t, `{"$m":[[1,"x"]]}`)
	if err := d.BeginObject(); err != nil {
		t.Fatal(err)
	}
	d.More()
	if _, err := d.Key(); err == nil || !strings.Contains(err.Error(), "BeginMap") {
		t.Errorf("non-string key: %v", err)
	}

	// $m as pairs, any key kind.
	d = dec(t, `{"$m":[[1,"a"],[{"$b":0},{"$s":[true]}],["s",null]]}`, []byte("k"))
	if err := d.BeginMap(); err != nil {
		t.Fatal(err)
	}
	n := 0
	for d.More() {
		if err := d.BeginArray(); err != nil {
			t.Fatal(err)
		}
		if err := d.Skip(); err != nil {
			t.Fatal(err)
		}
		if err := d.Skip(); err != nil {
			t.Fatal(err)
		}
		if err := d.EndArray(); err != nil {
			t.Fatal(err)
		}
		n++
	}
	if err := d.EndArray(); err != nil || n != 3 {
		t.Fatalf("pairs: %d %v", n, err)
	}
	if err := d.Finish(); err != nil {
		t.Fatal(err)
	}
	// BeginArray also reads a map as its list of pairs.
	d = dec(t, `{"$m":[["a",1]]}`)
	if err := d.BeginArray(); err != nil {
		t.Fatal(err)
	}
	if err := d.BeginArray(); err != nil {
		t.Fatal(err)
	}
	if err := d.Skip(); err != nil {
		t.Fatal(err)
	}
	if err := d.Skip(); err != nil {
		t.Fatal(err)
	}
	if err := d.EndArray(); err != nil {
		t.Fatal(err)
	}
	if err := d.EndArray(); err != nil || d.Finish() != nil {
		t.Fatal(err)
	}
	// Malformed pairs.
	for _, js := range []string{`{"$m":[["a"]]}`, `{"$m":[["a",1,2]]}`, `{"$m":["a"]}`, `{"$m":{}}`} {
		d := dec(t, js)
		err := d.BeginObject()
		if err == nil {
			for err == nil && d.More() {
				if _, err = d.Key(); err == nil {
					err = d.Skip()
				}
			}
			if err == nil {
				err = d.EndObject()
			}
		}
		if err == nil {
			t.Errorf("%s: want error", js)
		}
	}
}

// node is a value of the property test's own small model.
type node struct {
	kind string // null bool int float str bytes time array object
	b    bool
	i    int64
	f    float64
	s    string
	raw  []byte
	t    time.Time
	arr  []node
	keys []string
	vals []node
}

var tagKeys = []string{"$b", "$d", "$m", "$s", "$b$", "$d$", "$m$", "$s$", "x", "", "$", "$$", "$x"}

func randString(r *rand.Rand) string {
	pool := []string{"a", "Z", " ", "\"", "\\", "/", "\n", "\t", "\x00", "\x1f", "\x7f", "\u00e9", "\u2028", "\u2029",
		"\u65e5", "\U0001F600", "$", "\ufffd", "\uffff"}
	var sb strings.Builder
	for n := r.IntN(6); n > 0; n-- {
		sb.WriteString(pool[r.IntN(len(pool))])
	}
	return sb.String()
}

func randNode(r *rand.Rand, depth int) node {
	max := 10
	if depth > 4 {
		max = 7
	}
	switch r.IntN(max) {
	case 0:
		return node{kind: "null"}
	case 1:
		return node{kind: "bool", b: r.IntN(2) == 0}
	case 2:
		return node{kind: "int", i: int64(r.Uint64()) >> uint(r.IntN(64)) * int64(1-2*r.IntN(2))}
	case 3:
		var f float64
		for {
			f = math.Float64frombits(r.Uint64())
			if !math.IsNaN(f) && !math.IsInf(f, 0) {
				break
			}
		}
		if r.IntN(3) == 0 {
			f = float64(r.IntN(2000)-1000) / 8
		}
		return node{kind: "float", f: f}
	case 4:
		return node{kind: "str", s: randString(r)}
	case 5:
		b := make([]byte, r.IntN(5))
		for i := range b {
			b[i] = byte(r.IntN(256))
		}
		return node{kind: "bytes", raw: b}
	case 6:
		ms := r.Int64N(253402300800000) // years 1970..9999
		return node{kind: "time", t: time.UnixMilli(ms).UTC()}
	case 7, 8:
		a := node{kind: "array"}
		for n := r.IntN(4); n > 0; n-- {
			a.arr = append(a.arr, randNode(r, depth+1))
		}
		return a
	}
	o := node{kind: "object"}
	n := r.IntN(4)
	if r.IntN(2) == 0 {
		n = 1
	}
	for ; n > 0; n-- {
		k := randString(r)
		if r.IntN(2) == 0 {
			k = tagKeys[r.IntN(len(tagKeys))]
		}
		o.keys = append(o.keys, k)
		o.vals = append(o.vals, randNode(r, depth+1))
	}
	return o
}

func (n node) encode(e *Encoder) {
	switch n.kind {
	case "null":
		e.Null()
	case "bool":
		e.Bool(n.b)
	case "int":
		e.Int(n.i)
	case "float":
		e.Float(n.f)
	case "str":
		e.String(n.s)
	case "bytes":
		e.Bytes(n.raw)
	case "time":
		e.Time(n.t)
	case "array":
		e.BeginArray()
		for _, x := range n.arr {
			x.encode(e)
		}
		e.EndArray()
	case "object":
		e.BeginObject()
		for i, k := range n.keys {
			e.Key(k)
			n.vals[i].encode(e)
		}
		e.EndObject()
	}
}

func validString(s string) string { return strings.ToValidUTF8(s, "\ufffd") }

// check reads the next value from d, driven by n, and reports a mismatch.
func (n node) check(t *testing.T, d *Decoder) {
	t.Helper()
	want := map[string]Kind{"null": KindNull, "bool": KindBool, "int": KindNumber, "float": KindNumber, "str": KindString,
		"bytes": KindBytes, "time": KindTime, "array": KindArray, "object": KindObject}[n.kind]
	if got := d.Kind(); got != want {
		t.Fatalf("kind of %s: got %v want %v", n.kind, got, want)
	}
	switch n.kind {
	case "null":
		if !d.IsNull() {
			t.Fatal("IsNull")
		}
	case "bool":
		if v, err := d.Bool(); err != nil || v != n.b {
			t.Fatalf("bool %v %v", v, err)
		}
	case "int":
		if v, err := d.Int(); err != nil || v != n.i {
			t.Fatalf("int %d %v want %d", v, err, n.i)
		}
	case "float":
		if v, err := d.Float(); err != nil || v != n.f {
			t.Fatalf("float %v %v want %v", v, err, n.f)
		}
	case "str":
		if v, err := d.String(); err != nil || v != validString(n.s) {
			t.Fatalf("string %q %v want %q", v, err, n.s)
		}
	case "bytes":
		if v, err := d.Bytes(); err != nil || string(v) != string(n.raw) {
			t.Fatalf("bytes %x %v want %x", v, err, n.raw)
		}
	case "time":
		if v, err := d.Time(); err != nil || !v.Equal(n.t) {
			t.Fatalf("time %v %v want %v", v, err, n.t)
		}
	case "array":
		if err := d.BeginArray(); err != nil {
			t.Fatal(err)
		}
		for _, x := range n.arr {
			if !d.More() {
				t.Fatal("array ended early")
			}
			x.check(t, d)
		}
		if d.More() {
			t.Fatal("array has extra members")
		}
		if err := d.EndArray(); err != nil {
			t.Fatal(err)
		}
	case "object":
		if err := d.BeginObject(); err != nil {
			t.Fatal(err)
		}
		for i, k := range n.keys {
			if !d.More() {
				t.Fatal("object ended early")
			}
			got, err := d.Key()
			if err != nil || got != validString(k) {
				t.Fatalf("key %q %v want %q", got, err, k)
			}
			n.vals[i].check(t, d)
		}
		if d.More() {
			t.Fatal("object has extra members")
		}
		if err := d.EndObject(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPropertyRoundTrip(t *testing.T) {
	for seed := uint64(1); seed <= 4; seed++ {
		r := rand.New(rand.NewPCG(seed, seed*7919))
		for i := 0; i < 3000; i++ {
			n := randNode(r, 0)
			e := NewEncoder()
			n.encode(e)
			fr, err := e.Finish()
			if err != nil {
				t.Fatalf("seed %d #%d: %v", seed, i, err)
			}
			d, err := NewDecoder(fr)
			if err != nil {
				t.Fatalf("seed %d #%d: %v", seed, i, err)
			}
			n.check(t, d)
			if err := d.Finish(); err != nil {
				t.Fatalf("seed %d #%d: %v (%s)", seed, i, err, fr)
			}
			// A second pass through Skip/RawValue must agree on the span.
			d2, _ := NewDecoder(fr)
			if err := d2.Skip(); err != nil || d2.Finish() != nil {
				t.Fatalf("seed %d #%d: skip: %v (%s)", seed, i, err, fr)
			}
		}
	}
}
