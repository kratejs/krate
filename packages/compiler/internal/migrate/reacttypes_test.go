package migrate

import (
	"strings"
	"testing"
)

func bridgeFile(rep *Report) (PlannedFile, bool) {
	for _, f := range rep.Files {
		if f.Target == "src/krate-react.d.ts" {
			return f, true
		}
	}
	return PlannedFile{}, false
}

func TestMigrateAddsReactTypesBridgeWhenTypesReactKept(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "package.json", `{"dependencies":{"next":"15.0.0","react":"19.0.0"},"devDependencies":{"@types/react":"19.0.0"}}`)
	writeNextFile(t, root, "src/app/page.tsx", `export default function P() { return <main>hi</main>; }`)
	writeNextFile(t, root, "tsconfig.json", `{"compilerOptions":{"jsx":"preserve"}}`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := bridgeFile(rep)
	if !ok {
		t.Fatalf("expected src/krate-react.d.ts in the plan, got %v", rep.Files)
	}
	if !strings.Contains(string(f.Content), `import "@krate/runtime/react-types";`) {
		t.Errorf("bridge content: %s", f.Content)
	}
}

func TestMigrateSkipsReactTypesBridgeWithoutTypesReact(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "package.json", `{"dependencies":{"next":"15.0.0","react":"19.0.0"}}`)
	writeNextFile(t, root, "src/app/page.tsx", `export default function P() { return <main>hi</main>; }`)
	writeNextFile(t, root, "tsconfig.json", `{"compilerOptions":{"jsx":"preserve"}}`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := bridgeFile(rep); ok {
		t.Fatal("bridge must not be added when @types/react is absent")
	}
}
