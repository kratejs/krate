package bundler

import (
	"encoding/json"
	"testing"
)

func TestJSONDiagnosticLocations(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		line, col int
		source    string
	}{
		{"syntax error", "{\n  \"a\": 1,\n  \"b\": oops\n}\n", 3, 8, `  "b": oops`},
		{"truncated", "{\n  \"a\": 1,\n", 3, 1, ""},
		{"trailing comma", "{\n  \"a\": 1,\n}\n", 3, 1, "}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.src)
			err := json.Unmarshal(data, new(any))
			if err == nil {
				t.Fatal("fixture is valid JSON")
			}
			d := jsonDiagnostic("x.json", data, err)
			if d.Line != tc.line || d.Col != tc.col || d.Source != tc.source {
				t.Errorf("got %d:%d %q, want %d:%d %q", d.Line, d.Col, d.Source, tc.line, tc.col, tc.source)
			}
		})
	}
}
