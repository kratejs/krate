package config

import (
	"strings"
	"testing"
)

func TestAPIRuntimeScriptcValidation(t *testing.T) {
	for _, rt := range []string{"", "quickjs", "node", "bun", "deno", "scriptc", "Scriptc"} {
		c := Default()
		c.SSR.APIRuntime = rt
		if _, err := c.Validate(); err != nil {
			t.Errorf("apiRuntime %q must be valid: %v", rt, err)
		}
	}
	c := Default()
	c.SSR.APIRuntime = "scritpc"
	_, err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "scriptc") || !strings.Contains(err.Error(), "scritpc") ||
		!strings.Contains(err.Error(), "hint:") || !strings.Contains(err.Error(), `"quickjs", "node", "bun", "deno"`) {
		t.Errorf("a misspelled runtime must name the choices and the value, got %v", err)
	}
}

func TestScriptcStrictValidation(t *testing.T) {
	c := Default()
	c.SSR.APIRuntime = "scriptc"
	c.SSR.ScriptcStrict = true
	if w, err := c.Validate(); err != nil || len(w) != 0 {
		t.Errorf("strict with scriptc: warnings %v, err %v", w, err)
	}
	c = Default()
	c.SSR.ScriptcStrict = true
	w, err := c.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(w, "\n"), "scriptcStrict") {
		t.Errorf("strict without scriptc must warn, got %v", w)
	}
}

func TestMiddlewareRuntimeRejectsScriptc(t *testing.T) {
	c := Default()
	c.SSR.MiddlewareRuntime = "scriptc"
	if _, err := c.Validate(); err == nil || !strings.Contains(err.Error(), "middlewareRuntime") {
		t.Errorf("middlewareRuntime scriptc must be an error, got %v", err)
	}
}

func TestLoadScriptcConfig(t *testing.T) {
	root := writeConfig(t, `export default { ssr: { apiRuntime: "scriptc", scriptcStrict: true } };`)
	Warnings = nil
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SSR.APIRuntime != "scriptc" || !cfg.SSR.ScriptcStrict {
		t.Fatalf("ssr = %+v", cfg.SSR)
	}
	for _, w := range Warnings {
		if strings.Contains(w, "scriptc") {
			t.Errorf("known keys must not warn: %s", w)
		}
	}
}
