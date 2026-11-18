package migrate

import (
	"strings"
	"testing"
)

// TestPublicEnvDefinitionsFromEnvDirAndExample: the variables live in the
// monorepo root's .env.example, which next.config's loadEnvConfig points at.
func TestPublicEnvDefinitionsFromEnvDirAndExample(t *testing.T) {
	top := t.TempDir()
	root := top + "/apps/web"
	writeNextFile(t, root, "package.json", `{"dependencies":{"next":"16.0.0"}}`)
	writeNextFile(t, top, ".env.example", "NEXT_PUBLIC_UMAMI_URL=\nNEXT_PUBLIC_TURNSTILE_KEY=\n")
	writeNextFile(t, root, "next.config.ts", `import { loadEnvConfig } from "@next/env";
import path from "node:path";
loadEnvConfig(path.resolve(__dirname, "../.."));
export default {};`)
	writeNextFile(t, root, "app/page.tsx", `export default function P() { return <p>{process.env.NEXT_PUBLIC_UMAMI_URL}{process.env.NEXT_PUBLIC_TURNSTILE_KEY}</p>; }`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	info := strings.Join(rep.Info, "\n")
	if !strings.Contains(info, "defined in ../../.env.example: 2 of 2") {
		t.Errorf("definitions in the envDir not found: %q (manual %+v)", info, rep.Manual)
	}
	page := findPlan(rep, "src/pages/index.tsx")
	if page == nil || !strings.Contains(string(page.Content), "process.env.NEXT_PUBLIC_UMAMI_URL") {
		t.Errorf("NEXT_PUBLIC_ read was renamed: %+v", page)
	}
	cfg := findPlan(rep, "krate.config.ts")
	if cfg == nil || !strings.Contains(string(cfg.Content), `"envDir": "../.."`) || !strings.Contains(string(cfg.Content), `"NEXT_PUBLIC_"`) {
		t.Errorf("config lacks envDir/envPrefix: %+v", cfg)
	}
}

// TestNoEnvPrefixWhenNothingReadsNextPublic: an app without NEXT_PUBLIC_ reads
// gets no envPrefix and no info line.
func TestNoEnvPrefixWhenNothingReadsNextPublic(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "package.json", `{"dependencies":{"next":"16.0.0"}}`)
	writeNextFile(t, root, ".env", "NEXT_PUBLIC_UNUSED=zzz\n")
	writeNextFile(t, root, "app/page.tsx", `export default function P() { return <p>hi</p>; }`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg := findPlan(rep, "krate.config.ts"); cfg != nil && strings.Contains(string(cfg.Content), "envPrefix") {
		t.Errorf("envPrefix written without NEXT_PUBLIC_ reads:\n%s", cfg.Content)
	}
	if len(rep.Info) != 0 {
		t.Errorf("unexpected info: %v", rep.Info)
	}
}

// TestPublicEnvDestructuringNeedsNoManualItem: the names are unchanged, so
// destructuring is no longer a migration hazard of its own.
func TestPublicEnvDestructuringNeedsNoManualItem(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "package.json", `{"dependencies":{"next":"16.0.0"}}`)
	writeNextFile(t, root, "app/page.tsx", `const { NEXT_PUBLIC_X } = process.env;
export default function P() { return <p>{NEXT_PUBLIC_X}</p>; }`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "destructured") {
			t.Errorf("destructuring left a manual item: %+v", m)
		}
	}
}
