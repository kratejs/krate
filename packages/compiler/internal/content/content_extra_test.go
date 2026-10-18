package content

import "testing"

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
