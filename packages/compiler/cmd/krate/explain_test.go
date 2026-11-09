package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeCode(t *testing.T) {
	for in, want := range map[string]string{"KR7110": "KR7110", "kr7110": "KR7110", "7110": "KR7110", " KR-7110 ": "KR7110"} {
		got, ok := normalizeCode(in)
		if !ok || string(got) != want {
			t.Errorf("normalizeCode(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "KR71", "KR71100", "abc", "KR7110x"} {
		if _, ok := normalizeCode(in); ok {
			t.Errorf("normalizeCode(%q) accepted", in)
		}
	}
}

func TestExplainKnownCode(t *testing.T) {
	var out bytes.Buffer
	if err := writeExplain(&out, "kr7110", false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"KR7110", "https://krate.js.org/docs/reference/diagnostics/#kr7110"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output misses %q:\n%s", want, out.String())
		}
	}
}

func TestExplainJSON(t *testing.T) {
	var out bytes.Buffer
	if err := writeExplain(&out, "KR7110", true); err != nil {
		t.Fatal(err)
	}
	var e explainEntry
	if err := json.Unmarshal(out.Bytes(), &e); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if e.Code != "KR7110" || e.Title == "" || e.Explanation == "" || !strings.HasSuffix(e.DocsURL, "#kr7110") {
		t.Errorf("unexpected entry %+v", e)
	}
}

func TestExplainUnknownCode(t *testing.T) {
	var out bytes.Buffer
	err := writeExplain(&out, "KR7999", false)
	if err == nil || !strings.Contains(err.Error(), "KR7999") || !strings.Contains(err.Error(), "nearest codes: KR") {
		t.Fatalf("want an error that names the code and its neighbours, got %v", err)
	}
	if err := writeExplain(&out, "oops", false); err == nil {
		t.Fatal("non-code argument must fail")
	}
}
