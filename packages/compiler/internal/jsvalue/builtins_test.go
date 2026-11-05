package jsvalue

import (
	"strings"
	"testing"
	"time"
)

// Without Interp.Volatile an expression that depends on the clock, the time
// zone or the locale has no build-time value, since the browser computes it
// again; deterministic Date operations still fold.
func TestDateConstantsOnlyFoldWhenDeterministic(t *testing.T) {
	for _, src := range []string{
		`new Date()`, `new Date().getFullYear()`, `Date.now()`, `Date()`, `Math.random()`,
		`new Date(0).getHours()`, `new Date(0).getDate()`, `new Date(0).toString()`, `new Date(0).toDateString()`,
		`new Date(0).toLocaleDateString()`, `new Date(0).toLocaleString("en-US")`,
		`new Date(2020, 0, 1).getTime()`, `new Date("2020-01-01T10:00").getTime()`, `new Date("March 5, 2020").getTime()`,
		`Date.parse("2020-01-01T10:00")`,
		`String(new Date(0))`, "`${new Date(0)}`", `new Date(0) + ""`, `new Date(0) == "x"`,
	} {
		if v := evalSrc(t, src); !v.IsUnknown() || v.IsUnsupported() {
			t.Errorf("%s = %#v; want a dynamic value", src, v)
		}
	}
	for src, want := range map[string]string{
		`new Date(0).toISOString()`:                       "1970-01-01T00:00:00.000Z",
		`new Date("2020-01-01").getUTCFullYear()`:         "2020",
		`new Date("2020-01-01T10:00:00Z").getTime()`:      "1577872800000",
		`new Date("2020-01-01T10:00:00+02:00").getTime()`: "1577865600000",
		`Date.UTC(2020, 0, 1)`:                            "1577836800000",
		`new Date(5).getTime() - new Date(2).getTime()`:   "3",
		`new Date(5) - new Date(2)`:                       "3",
		`new Date(1) < new Date(2)`:                       "true",
		`new Date(0).toUTCString()`:                       "Thu, 01 Jan 1970 00:00:00 GMT",
		`JSON.stringify({ d: new Date(0) })`:              `{"d":"1970-01-01T00:00:00.000Z"}`,
		`typeof new Date(0)`:                              "object",
	} {
		if got := evalSrc(t, src); got.IsUnknown() || got.ToString() != want {
			t.Errorf("%s = %#v, want %q", src, got, want)
		}
	}
}

func TestVolatileDateUsesTheBuildClock(t *testing.T) {
	SetBuildClock(time.Date(2024, 2, 29, 23, 59, 58, 123_000_000, time.UTC))
	defer ResetBuildClock()
	for src, want := range map[string]string{
		`new Date().toISOString()`:                    "2024-02-29T23:59:58.123Z",
		`Date.now()`:                                  "1709251198123",
		`new Date().getFullYear()`:                    "2024",
		`new Date().getDate()`:                        "29",
		`new Date().getDay()`:                         "4",
		`Date()`:                                      "Thu Feb 29 2024 23:59:58 GMT+0000 (Coordinated Universal Time)",
		`new Date().toLocaleDateString()`:             "2/29/2024",
		`new Date(Date.now() + 86400000).getMonth()`:  "2",
		`new Date() - new Date(Date.UTC(2024, 0, 1))`: "5183998123",
	} {
		if got := evalSrcVolatile(t, src, true); got.IsUnknown() || got.ToString() != want {
			t.Errorf("%s = %#v, want %q", src, got, want)
		}
	}
}

func TestSourceDateEpoch(t *testing.T) {
	ResetBuildClock()
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	if got := evalSrcVolatile(t, `Date.now()`, true).ToString(); got != "1700000000000" {
		t.Errorf("Date.now() with SOURCE_DATE_EPOCH = %s", got)
	}
	if got := evalSrcVolatile(t, `new Date().toISOString()`, true).ToString(); got != "2023-11-14T22:13:20.000Z" {
		t.Errorf("new Date() with SOURCE_DATE_EPOCH = %s", got)
	}
	// StartBuildClock fixes the instant for the whole build.
	if got := StartBuildClock().Unix(); got != 1700000000 {
		t.Errorf("StartBuildClock = %d", got)
	}
	ResetBuildClock()
	for _, bad := range []string{"", "abc", "1.5", "99999999999999999999"} {
		t.Setenv("SOURCE_DATE_EPOCH", bad)
		if _, ok := SourceDateEpoch(); ok {
			t.Errorf("SOURCE_DATE_EPOCH=%q was accepted", bad)
		}
	}
}

func TestMathRandomIsOnlyKnownWhereVolatile(t *testing.T) {
	ResetBuildClock()
	v := evalSrcVolatile(t, `Math.random()`, true)
	if v.IsUnknown() || v.NumberValue() < 0 || v.NumberValue() >= 1 {
		t.Fatalf("Math.random() = %#v", v)
	}
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	a := evalSrcVolatile(t, `[Math.random(), Math.random(), Math.random()].join()`, true).ToString()
	b := evalSrcVolatile(t, `[Math.random(), Math.random(), Math.random()].join()`, true).ToString()
	if a != b {
		t.Errorf("Math.random() with SOURCE_DATE_EPOCH differs between runs: %s vs %s", a, b)
	}
	parts := strings.Split(a, ",")
	if parts[0] == parts[1] || parts[1] == parts[2] {
		t.Errorf("calls at different sites repeat a value: %s", a)
	}
	t.Setenv("SOURCE_DATE_EPOCH", "1700000001")
	if c := evalSrcVolatile(t, `[Math.random(), Math.random(), Math.random()].join()`, true).ToString(); c == a {
		t.Errorf("a different SOURCE_DATE_EPOCH gave the same numbers")
	}
}

func TestIteratorsAreIteratorsNotArrays(t *testing.T) {
	for src, want := range map[string]string{
		`typeof [1].entries().next`:                                   "function",
		`Array.isArray([1].entries())`:                                "false",
		`Array.isArray([1].keys().toArray())`:                         "true",
		`[1].entries().length`:                                        "undefined",
		`typeof new Map().keys().next`:                                "function",
		`typeof new Set().values().next`:                              "function",
		`new Map([[1, 2]]).entries().next().value[1]`:                 "2",
		`new Set([5]).values().next().done`:                           "false",
		`typeof [1].values()[0]`:                                      "undefined",
		`(() => { const it = [1, 2].values(); return it === it; })()`: "true",
		`[1].values() === [1].values()`:                               "false",
	} {
		if got := evalSrc(t, src); got.IsUnknown() || got.ToString() != want {
			t.Errorf("%s = %#v, want %q", src, got, want)
		}
	}
}

// Members the evaluator does not implement stay unresolved instead of reading
// as undefined, and unknown values propagate through the new collections.
func TestBuiltinGapsStayUnresolved(t *testing.T) {
	for _, src := range []string{
		`typeof new Set([1]).union`, `new Set([1]).isSubsetOf`, `typeof new Date(0).setYear`,
		`typeof [1].values().constructor`, `typeof new Map().constructor`, `new Date(0).toLocaleDateString("de-DE")`,
		`new Date("not a known format 12").getTime()`, `new Map([[1, later]]).get(1)`, `[...later]`,
		`new Set([...later]).size`, `new Map(later).size`,
	} {
		if v := evalSrcVolatile(t, src, true); !v.IsUnknown() {
			t.Errorf("%s folded to %q", src, v.ToString())
		}
	}
}

func TestDateStaysSameAcrossCopiesAndCollections(t *testing.T) {
	for src, want := range map[string]string{
		`(() => { const d = new Date(0); const e = new Date(d); e.setUTCDate(3); return d.getUTCDate() + "," + e.getUTCDate(); })()`: "1,3",
		`(() => { const d = new Date(0); const m = new Map([[d, "a"]]); return m.get(d) + m.get(new Date(0)); })()`:                  "aundefined",
		`[new Date(3), new Date(1), new Date(2)].sort((a, b) => a - b).map(d => d.getTime()).join()`:                                 "1,2,3",
	} {
		if got := evalSrcVolatile(t, src, true); got.IsUnknown() || got.ToString() != want {
			t.Errorf("%s = %#v, want %q", src, got, want)
		}
	}
}
