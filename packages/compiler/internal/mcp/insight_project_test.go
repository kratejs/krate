package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEnvCheckNeverReturnsValues(t *testing.T) {
	const secret = "s3cr3t-value-DO-NOT-LEAK"
	const plain = "plain-visible-looking-value"
	svc := newTestService(t, map[string]string{
		".env":       "DATABASE_URL=" + secret + "\nSTRIPE_SECRET=" + secret + "\nUNUSED_FLAG=" + plain + "\nKRATE_PUBLIC_API_KEY=" + secret + "\nKRATE_PUBLIC_SITE=" + plain + "\n# COMMENTED=1\n",
		".env.local": "OVERRIDE=" + plain + "\n",
		"src/pages/index.tsx": `const site = process.env.KRATE_PUBLIC_SITE;
export default function P() { return <h1>{import.meta.env.KRATE_PUBLIC_API_KEY}{process.env.NOT_PUBLIC}</h1>; }
`,
		"src/api/db.ts": "const url = process.env.DATABASE_URL; const k = process.env['STRIPE_SECRET']; const m = process.env.MISSING_ONE;\nexport function GET() { return new Response(url + k + m); }\n",
	})
	_, res := invoke(t, svc, "env_check", nil)
	raw := res.Content[0].Text
	if strings.Contains(raw, secret) || strings.Contains(raw, plain) {
		t.Fatalf("env_check leaked a value:\n%s", raw)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(raw), &m)
	if !contains(strs(list(t, m, "missing")), "MISSING_ONE") {
		t.Errorf("missing = %v", m["missing"])
	}
	if !contains(strs(list(t, m, "missing")), "NOT_PUBLIC") {
		t.Errorf("NOT_PUBLIC is neither declared nor set: %v", m["missing"])
	}
	if got := strs(list(t, m, "secretExposure")); len(got) != 1 || got[0] != "KRATE_PUBLIC_API_KEY" {
		t.Errorf("secretExposure = %v", got)
	}
	unused := strs(list(t, m, "unused"))
	if !contains(unused, "UNUSED_FLAG") || !contains(unused, "OVERRIDE") || contains(unused, "DATABASE_URL") || contains(unused, "COMMENTED") {
		t.Errorf("unused = %v", unused)
	}
	var db map[string]any
	for _, u := range list(t, m, "used") {
		if asMap(u)["name"] == "DATABASE_URL" {
			db = asMap(u)
		}
	}
	if db == nil || db["declared"] != true {
		t.Fatalf("DATABASE_URL = %v", db)
	}
	loc := asMap(list(t, db, "locations")[0])
	if loc["file"] != "src/api/db.ts" || loc["line"] != float64(1) {
		t.Errorf("location = %v", loc)
	}
	// Variables set in the shell count as provided, but only by name.
	t.Setenv("MISSING_ONE", "shell-value-hidden")
	_, res2 := invoke(t, svc, "env_check", nil)
	if strings.Contains(res2.Content[0].Text, "shell-value-hidden") {
		t.Fatal("a shell value leaked")
	}
	var m2 map[string]any
	_ = json.Unmarshal([]byte(res2.Content[0].Text), &m2)
	if contains(strs(list(t, m2, "missing")), "MISSING_ONE") {
		t.Error("a variable set in the shell is not missing")
	}
}
