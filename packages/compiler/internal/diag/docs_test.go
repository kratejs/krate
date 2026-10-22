package diag

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The diagnostics reference page is generated from the registry so it never
// drifts from the codes the compiler can emit. KRATE_DIAG_DOCS_UPDATE=1
// rewrites it; otherwise the test fails when the page is stale.

const diagDocsPath = "../../../web/src/content/docs/reference/diagnostics.md"

var rangeTitles = map[byte]string{
	'1': "KR1xxx: syntax and parsing",
	'2': "KR2xxx: React effects, dependency arrays and hooks",
	'3': "KR3xxx: class components and error boundaries",
	'4': "KR4xxx: Suspense, lazy, use() and transitions",
	'5': "KR5xxx: events, forms and refs",
	'6': "KR6xxx: lists and keys",
	'7': "KR7xxx: React 19 actions and server actions",
	'8': "KR8xxx: islands, compiled packages and general React compatibility",
}

// renderDiagnosticsDoc returns the markdown for the diagnostics reference.
func renderDiagnosticsDoc(codes []CodeInfo) string {
	var b strings.Builder
	b.WriteString(`---
title: Diagnostic Codes
description: Every stable KRxxxx diagnostic code the Krate compiler reports, with an explanation.
order: 5
---

<!-- Generated from internal/diag by TestDiagnosticsDocUpToDate. Do not edit by hand; run
     KRATE_DIAG_DOCS_UPDATE=1 go test ./internal/diag from packages/compiler. -->

# Diagnostic Codes

Krate diagnostics that point at a known compatibility limit carry a stable
code such as ` + "`KR2001`" + `. The code appears in the message
(` + "`file.tsx:3:5: [KR2001] ...`" + `), never changes meaning once released,
and is listed here with the reason it is reported and what to do instead.

Codes are grouped by area:

| Range | Area |
|-------|------|
`)
	for c := byte('1'); c <= '8'; c++ {
		title := rangeTitles[c]
		parts := strings.SplitN(title, ": ", 2)
		fmt.Fprintf(&b, "| `%s` | %s |\n", parts[0], parts[1])
	}
	byRange := map[byte][]CodeInfo{}
	for _, info := range codes {
		if len(info.Code) == 6 {
			byRange[info.Code[2]] = append(byRange[info.Code[2]], info)
		}
	}
	for c := byte('1'); c <= '8'; c++ {
		fmt.Fprintf(&b, "\n## %s\n\n", rangeTitles[c])
		list := byRange[c]
		if len(list) == 0 {
			b.WriteString("No codes are registered in this range yet.\n")
			continue
		}
		for i, info := range list {
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "### %s\n\n**%s**\n\n%s\n", info.Code, info.Title, info.Explanation)
		}
	}
	b.WriteString(`
## See also

- [React Compatibility](/docs/guides/react-compatibility/)
- [Compatibility matrix](/docs/guides/compatibility/)
`)
	return b.String()
}

func TestDiagnosticsDocUpToDate(t *testing.T) {
	want := renderDiagnosticsDoc(Codes())
	path := filepath.FromSlash(diagDocsPath)
	if os.Getenv("KRATE_DIAG_DOCS_UPDATE") != "" {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s (%d codes)", path, len(Codes()))
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run KRATE_DIAG_DOCS_UPDATE=1 go test ./internal/diag)", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s is stale; run KRATE_DIAG_DOCS_UPDATE=1 go test ./internal/diag from packages/compiler", path)
	}
}

func TestDiagnosticsDocListsEveryCode(t *testing.T) {
	doc := renderDiagnosticsDoc(Codes())
	for _, info := range Codes() {
		if !strings.Contains(doc, "### "+string(info.Code)+"\n") {
			t.Errorf("generated doc misses %s", info.Code)
		}
	}
}
