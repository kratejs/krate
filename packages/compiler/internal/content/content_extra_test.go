package content

import (
	"strings"
	"testing"
)

func TestApplyDefaultsAndEnum(t *testing.T) {
	schema := map[string]Field{
		"status": {Type: TypeString, Enum: []string{"draft", "published"}, Default: "draft"},
	}
	data := map[string]any{}
	ApplyDefaults(schema, data)
	if data["status"] != "draft" {
		t.Fatalf("default not applied: %v", data)
	}
	if errs := Validate(schema, data); len(errs) != 0 {
		t.Fatalf("default value should validate: %v", errs)
	}
	data["status"] = "bogus"
	if errs := Validate(schema, data); len(errs) == 0 {
		t.Fatal("expected an enum violation for 'bogus'")
	}
}

func TestSchemaDateFieldsMustBeISO(t *testing.T) {
	schema := ParseSchema(map[string]any{"published": "date"})
	for _, ok := range []string{"2024-01-31", "2024-01-31T09:30:00Z", "2024-01-31 09:30", "2024-02-29T00:00:00.123+02:00"} {
		if errs := Validate(schema, map[string]any{"published": ok}); len(errs) != 0 {
			t.Errorf("%q should be a valid date: %v", ok, errs)
		}
	}
	for _, bad := range []string{"yesterday", "2024-13-01", "2023-02-29", "31/01/2024", ""} {
		errs := Validate(schema, map[string]any{"published": bad})
		if len(errs) != 1 || !strings.Contains(errs[0].Error(), "ISO date") {
			t.Errorf("%q should be rejected with an ISO hint, got %v", bad, errs)
		}
	}
	if errs := Validate(schema, map[string]any{"published": int64(5)}); len(errs) != 1 {
		t.Errorf("a number is not a date: %v", errs)
	}
}

func TestGeneratedTypesUseEnumUnionsAndRequiredDefaults(t *testing.T) {
	cfg := &Config{Collections: map[string]Collection{"posts": {Dir: "p", Schema: ParseSchema(map[string]any{
		"status": map[string]any{"type": "string", "enum": []any{"draft", "live"}},
		"views":  map[string]any{"type": "number", "default": 0},
		"note":   "string",
		"when":   "date",
	})}}}
	out := Generate(cfg, nil)
	for _, want := range []string{`status?: "draft" | "live";`, "views: number;", "note?: string;", "when?: string;"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
