package diag

import (
	"regexp"
	"strings"
	"testing"
)

var codeRe = regexp.MustCompile(`^KR[1-8][0-9]{3}$`)

func TestRegisteredCodesAreWellFormed(t *testing.T) {
	for _, info := range Codes() {
		if !codeRe.MatchString(string(info.Code)) {
			t.Errorf("code %q does not match KR + four digits in a known range", info.Code)
		}
		if strings.TrimSpace(info.Title) == "" || strings.TrimSpace(info.Explanation) == "" {
			t.Errorf("code %s needs a title and an explanation", info.Code)
		}
	}
}

func TestCodeRendersInMessages(t *testing.T) {
	d := New("a.tsx", 3, 5, "deps must be an array literal", "write [a, b]", "useEffect(f, deps)").WithCode("KR2001")
	if got := d.Error(); got != "a.tsx:3:5: [KR2001] deps must be an array literal" {
		t.Fatalf("Error() = %q", got)
	}
	if got := FormatDiagnostics([]error{d}); !strings.HasPrefix(got, "a.tsx:3:5: [KR2001] deps") {
		t.Fatalf("FormatDiagnostics = %q", got)
	}
}
