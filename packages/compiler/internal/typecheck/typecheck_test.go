package typecheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const tscSource = `@server
export function A() { return null; }
@static
export function B() { return null; }
@client
export const C = () => null;
@server export class D {}
@sever
export class E {}
const z: number = "typo";
`

const tscOutput = `a.tsx(1,1): error TS1206: Decorators are not valid here.
a.tsx(3,1): error TS1206: Decorators are not valid here.
a.tsx(3,2): error TS1214: Identifier expected. 'static' is a reserved word in strict mode. Modules are automatically in strict mode.
a.tsx(5,1): error TS1206: Decorators are not valid here.
a.tsx(7,2): error TS2304: Cannot find name 'server'.
a.tsx(8,2): error TS2304: Cannot find name 'sever'.
a.tsx(10,7): error TS2322: Type 'string' is not assignable to type 'number'.
  Types of property 'x' are incompatible.
`

func read(string) (string, error) { return tscSource, nil }

func TestFilterKeepsOnlyRealErrors(t *testing.T) {
	kept, remaining, dropped := Filter(tscOutput, read)
	want := `a.tsx(8,2): error TS2304: Cannot find name 'sever'.
a.tsx(10,7): error TS2322: Type 'string' is not assignable to type 'number'.
  Types of property 'x' are incompatible.
`
	if kept != want {
		t.Errorf("kept:\n%s", kept)
	}
	if remaining != 2 || dropped != 5 {
		t.Errorf("remaining %d, dropped %d", remaining, dropped)
	}
}

func TestFilterKeepsContinuationsOnlyWithTheirError(t *testing.T) {
	output := "a.tsx(1,1): error TS1206: Decorators are not valid here.\n  detail\n"
	if kept, _, _ := Filter(output, read); kept != "" {
		t.Errorf("kept %q", kept)
	}
}

func TestFilterIgnoresOtherErrorsOnADecoratorLine(t *testing.T) {
	// A different column, or another code at the decorator, is not ours.
	output := "a.tsx(1,3): error TS1206: Decorators are not valid here.\na.tsx(1,1): error TS2322: x\n"
	if _, remaining, dropped := Filter(output, read); remaining != 2 || dropped != 0 {
		t.Errorf("remaining %d, dropped %d", remaining, dropped)
	}
}

func TestFilterCountsConfigErrors(t *testing.T) {
	if _, remaining, _ := Filter("error TS5058: The specified path does not exist.\n", read); remaining != 1 {
		t.Errorf("remaining = %d", remaining)
	}
}

func TestFindTSCWalksUp(t *testing.T) {
	root := t.TempDir()
	tsc := filepath.Join(root, "node_modules", "typescript", "bin", "tsc")
	if err := os.MkdirAll(filepath.Dir(tsc), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tsc, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(root, "apps", "web")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := FindTSC(app); got != tsc {
		t.Errorf("FindTSC = %q", got)
	}
}

func TestRunNeedsTypeScript(t *testing.T) {
	if strings.Contains(t.TempDir(), "node_modules") {
		t.Skip("temp dir is inside node_modules")
	}
	if FindTSC(t.TempDir()) == "" {
		if _, _, err := Run(t.TempDir()); err != ErrNoTSC {
			t.Errorf("err = %v", err)
		}
	}
}
