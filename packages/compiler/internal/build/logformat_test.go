package build

import (
	"strings"
	"testing"
	"time"
)

func TestFormatRequestLineNeutralisesControlCharacters(t *testing.T) {
	forged := "/\x1b[2J\n10:00:00 GET /forged 200 1ms"
	for _, color := range []bool{false, true} {
		line := formatRequestLine("GET", forged, 200, time.Millisecond, color)
		if strings.ContainsAny(line, "\n\r\x1b") && !color {
			t.Errorf("color=%v: control characters survived: %q", color, line)
		}
		if strings.Contains(line, "\n") {
			t.Errorf("color=%v: a request path must not add log lines: %q", color, line)
		}
		if !strings.Contains(line, "%1B[2J%0A10:00:00 GET /forged") {
			t.Errorf("color=%v: expected escaped path in %q", color, line)
		}
	}
	if got := safeLogText("/plain/ok é"); got != "/plain/ok é" {
		t.Errorf("clean text changed: %q", got)
	}
}
