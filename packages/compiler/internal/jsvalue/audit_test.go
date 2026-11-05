package jsvalue

import "testing"

func TestEvalAuditCases(t *testing.T) {
	str := func(v Value) string { return v.ToString() }
	cases := []struct{ src, want string }{
		{`({get v(){return 5}}).v`, "5"},
		{`({a: 2, get v(){return 5}}).v + 1`, "6"},
		{`"ß".toUpperCase()`, "SS"},
		{`"straße".toUpperCase()`, "STRASSE"},
		{`"ǆ".toUpperCase()`, "Ǆ"},
		{`"ŉ".toUpperCase()`, "ʼN"},
		{`"ﬁ".toUpperCase()`, "FI"},
		{`"İ".toLowerCase()`, "i̇"},
		{`"ΑΣ".toLowerCase()`, "ας"},
		{`"ΑΣ Β".toLowerCase()`, "ας β"},
		{`(255).toString(16)`, "ff"},
		{`(-255).toString(2)`, "-11111111"},
	}
	for _, tc := range cases {
		v := evalSrc(t, tc.src)
		if v.IsUnknown() || v.IsUnsupported() {
			t.Errorf("%s: unresolved %#v", tc.src, v)
			continue
		}
		if got := str(v); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.src, got, tc.want)
		}
	}
	for _, src := range []string{
		`"a".toLocaleUpperCase()`, `"a".toLocaleLowerCase()`, `(1234.5).toLocaleString()`,
		`"a".localeCompare("b")`,
	} {
		if v := evalSrc(t, src); !v.IsUnknown() && !v.IsUnsupported() {
			t.Errorf("%s: locale-dependent call was folded to %#v", src, v)
		}
	}
}

func TestEvalJSONStringifyIndent(t *testing.T) {
	cases := []struct{ src, want string }{
		{`JSON.stringify({a:1,b:[1,2],c:{}},null,2)`, "{\n  \"a\": 1,\n  \"b\": [\n    1,\n    2\n  ],\n  \"c\": {}\n}"},
		{`JSON.stringify([1,[]],null,"\t")`, "[\n\t1,\n\t[]\n]"},
		{`JSON.stringify({a:1},null,20)`, "{\n          \"a\": 1\n}"},
		{`JSON.stringify({a:1},null,0)`, `{"a":1}`},
		{`JSON.stringify({a:1,b:undefined},undefined,1)`, "{\n \"a\": 1\n}"},
		{`JSON.stringify({a:1,b:2})`, `{"a":1,"b":2}`},
	}
	for _, tc := range cases {
		v := evalSrc(t, tc.src)
		if v.IsUnknown() {
			t.Errorf("%s: unresolved %#v", tc.src, v)
			continue
		}
		if got := v.ToString(); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.src, got, tc.want)
		}
	}
	// Not implemented: must stay unresolved so the client computes it.
	for _, src := range []string{
		`JSON.stringify({a:{toJSON(){return 5}}})`,
		`JSON.stringify({a:1},["a"])`,
		`JSON.stringify({a:1},(k,v)=>v,2)`,
	} {
		if v := evalSrc(t, src); !v.IsUnknown() {
			t.Errorf("%s: folded to %#v", src, v)
		}
	}
}

func TestEvalMissingBuiltinMembersAreUnknown(t *testing.T) {
	cases := []struct{ src, want string }{
		{`Number.MAX_SAFE_INTEGER`, "9007199254740991"},
		{`Number.MAX_SAFE_INTEGER + 2`, "9007199254740992"},
		{`Number.MIN_SAFE_INTEGER`, "-9007199254740991"},
		{`Number.MAX_VALUE`, "1.7976931348623157e+308"},
		{`Number.MIN_VALUE`, "5e-324"},
		{`Number.EPSILON`, "2.220446049250313e-16"},
		{`Number.POSITIVE_INFINITY`, "Infinity"},
		{`Number.isInteger(3)`, "true"},
		{`Number.isInteger(3.5)`, "false"},
		{`Math.SQRT2`, "1.4142135623730951"},
		{`Math.SQRT1_2`, "0.7071067811865476"},
		{`Math.LN2`, "0.6931471805599453"},
		{`Math.LN10`, "2.302585092994046"},
		{`Math.LOG2E`, "1.4426950408889634"},
		{`Math.LOG10E`, "0.4342944819032518"},
	}
	for _, tc := range cases {
		v := evalSrc(t, tc.src)
		if v.IsUnknown() {
			t.Errorf("%s: unresolved %#v", tc.src, v)
			continue
		}
		if got := v.ToString(); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.src, got, tc.want)
		}
	}
	for _, src := range []string{`Math.atan2(1,2)`, `Math.NOPE`, `Number.NOPE`, `Number.parseFloat("1")`, `Object.nope`, `Array.nope`} {
		if v := evalSrc(t, src); !v.IsUnknown() {
			t.Errorf("%s: a missing built-in member evaluated to %#v", src, v)
		}
	}
	// A user object's missing property is still undefined.
	if v := evalSrc(t, `({a:1}).b`); v.Kind() != KindUndefined {
		t.Errorf("({a:1}).b = %#v, want undefined", v)
	}
}

func TestEvalForLetClosuresCaptureEachIteration(t *testing.T) {
	cases := []struct{ src, want string }{
		{`(()=>{let fs=[];for(let i=0;i<3;i++)fs.push(()=>i);return fs.map(f=>f()).join()})()`, "0,1,2"},
		{`(()=>{let fs=[];for(var i=0;i<3;i++)fs.push(()=>i);return fs.map(f=>f()).join()})()`, "3,3,3"},
		{`(()=>{let fs=[];for(let i=0,j=10;i<3;i++,j--)fs.push(()=>i+j);return fs.map(f=>f()).join()})()`, "10,10,10"},
		{`(()=>{let s=0;for(let i=0;i<4;i++){if(i==2)continue;s+=i}return s})()`, "4"},
	}
	for _, tc := range cases {
		v := evalSrc(t, tc.src)
		if v.IsUnknown() {
			t.Errorf("%s: unresolved %#v", tc.src, v)
			continue
		}
		if got := v.ToString(); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.src, got, tc.want)
		}
	}
}

func TestEvalArrayConstruction(t *testing.T) {
	cases := []struct{ src, want string }{
		{`[...Array(3)].length`, "3"},
		{`Array(3).length`, "3"},
		{`new Array(2).length`, "2"},
		{`[...Array(3)].map((_, k) => k).join()`, "0,1,2"},
		{`Array(3).fill(7).join("-")`, "7-7-7"},
		{`Array(1, 2).join()`, "1,2"},
		{`Array.from({length: 3}, (_, k) => k * 2).join()`, "0,2,4"},
		{`Array.isArray(Array(2))`, "true"},
		{`Array.from(Array(3).keys()).join()`, "0,1,2"},
	}
	for _, tc := range cases {
		v := evalSrc(t, tc.src)
		if v.IsUnknown() {
			t.Errorf("%s: unresolved %#v", tc.src, v)
			continue
		}
		if got := v.ToString(); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.src, got, tc.want)
		}
	}
	if v := evalSrc(t, `Array(-1)`); !v.IsUnknown() {
		t.Errorf("Array(-1) folded to %#v", v)
	}
}
