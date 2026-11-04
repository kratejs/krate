package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// `krate inspect --json` must print only the JSON document on stdout; build
// progress belongs on stderr or nowhere.
func TestInspectJSONStdoutIsPureJSON(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "krate")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "pages"), 0o755); err != nil {
		t.Fatal(err)
	}
	page := "export default function Page() { return <h1>hi</h1>; }\n"
	if err := os.WriteFile(filepath.Join(root, "src", "pages", "index.tsx"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "inspect", "--json", root)
	var stdout, stderr []byte
	var err error
	stdout, err = cmd.Output()
	if ee, ok := err.(*exec.ExitError); ok {
		stderr = ee.Stderr
	}
	if err != nil {
		t.Fatalf("inspect failed: %v\n%s", err, stderr)
	}
	var v map[string]any
	if err := json.Unmarshal(stdout, &v); err != nil {
		t.Fatalf("stdout is not a single JSON document: %v\n%s", err, stdout)
	}
	if _, ok := v["routes"]; !ok {
		t.Fatalf("no routes in output: %s", stdout)
	}
}
