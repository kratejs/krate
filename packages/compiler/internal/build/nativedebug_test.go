package build

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/native/runtimes"
)

func TestFindDlvMissingHintsAtInstall(t *testing.T) {
	goExe, _ := exec.LookPath("go")
	if goExe != "" {
		t.Setenv("PATH", filepath.Dir(goExe))
	} else {
		t.Setenv("PATH", t.TempDir())
	}
	t.Setenv(DlvPathEnv, "")
	t.Setenv("GOBIN", t.TempDir())
	t.Setenv("GOPATH", t.TempDir())
	_, err := FindDlv()
	if err == nil || !strings.Contains(err.Error(), "go install github.com/go-delve/delve/cmd/dlv@latest") {
		t.Fatalf("a missing dlv must say how to install it: %v", err)
	}
}

func TestFindDlvSources(t *testing.T) {
	dir := t.TempDir()
	name := "dlv"
	if runtime.GOOS == "windows" {
		name += ".exe" // what PATH and GOBIN lookups find there
	}
	exe := filepath.Join(dir, name)
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(DlvPathEnv, exe)
	if got, err := FindDlv(); err != nil || got != exe {
		t.Fatalf("env override: %q %v", got, err)
	}
	t.Setenv(DlvPathEnv, filepath.Join(dir, "nope"))
	if _, err := FindDlv(); err == nil {
		t.Fatal("a bad $KRATE_DLV is an error, not a fallback")
	}
	t.Setenv(DlvPathEnv, "")
	t.Setenv("PATH", dir)
	if got, err := FindDlv(); err != nil || got != exe {
		t.Fatalf("PATH: %q %v", got, err)
	}
	// go install puts it in GOBIN.
	goExe, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}
	t.Setenv("PATH", filepath.Dir(goExe))
	t.Setenv("GOBIN", dir)
	if got, err := FindDlv(); err != nil || got != exe {
		t.Fatalf("GOBIN: %q %v", got, err)
	}
}

func TestResolveDebugAddr(t *testing.T) {
	if _, _, err := ResolveDebugAddr("0.0.0.0:2345"); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("a network address must be refused: %v", err)
	}
	if _, _, err := ResolveDebugAddr("example.com:2345"); err == nil {
		t.Fatal("a host name is refused")
	}
	if _, _, err := ResolveDebugAddr("127.0.0.1:99999"); err == nil {
		t.Fatal("a bad port is refused")
	}
	if _, _, err := ResolveDebugAddr("nonsense"); err == nil {
		t.Fatal("no port is refused")
	}
	addr, fell, err := ResolveDebugAddr("127.0.0.1:0")
	if err != nil || fell || strings.HasSuffix(addr, ":0") {
		t.Fatalf("port 0 picks a free port: %q %v %v", addr, fell, err)
	}
	// A taken explicit port is an error; a taken default falls back.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if _, _, err := ResolveDebugAddr(l.Addr().String()); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("explicit busy port: %v", err)
	}
	if a, _, err := ResolveDebugAddr("localhost:0"); err != nil || !strings.HasPrefix(a, "localhost:") {
		t.Fatalf("localhost: %q %v", a, err)
	}
}

func TestInspectAddrsAndCommands(t *testing.T) {
	if got := InspectAddrs("127.0.0.1:9229", 3); !reflect.DeepEqual(got, []string{"127.0.0.1:9229", "127.0.0.1:9230", "127.0.0.1:9231"}) {
		t.Fatalf("%v", got)
	}
	if InspectAddrs("", 2) != nil || InspectAddrs("bad", 2) != nil {
		t.Fatal("no base, no addresses")
	}
	node := runtimes.WithInspect(runtimes.Command(runtimes.Node, nil), runtimes.Node, "127.0.0.1:9229")
	if !reflect.DeepEqual(node, []string{"node", "--inspect=127.0.0.1:9229", "--enable-source-maps", "{entry}"}) {
		t.Fatalf("node: %v", node)
	}
	bun := runtimes.WithInspect(runtimes.Command(runtimes.Bun, nil), runtimes.Bun, "127.0.0.1:9230")
	if !reflect.DeepEqual(bun, []string{"bun", "run", "--inspect=127.0.0.1:9230", "{entry}"}) {
		t.Fatalf("bun: %v", bun)
	}
	deno := runtimes.WithInspect(runtimes.Command(runtimes.Deno, []string{"fs"}), runtimes.Deno, "127.0.0.1:9231")
	if deno[0] != "deno" || deno[1] != "run" || deno[2] != "--inspect=127.0.0.1:9231" || deno[len(deno)-1] != "{entry}" {
		t.Fatalf("deno: %v", deno)
	}
}

func TestMergeLaunchJSON(t *testing.T) {
	cfg := []map[string]any{VSCodeGoAttach("127.0.0.1:2345")}
	out, ok := MergeLaunchJSON(nil, cfg)
	if !ok {
		t.Fatal("a new file")
	}
	var doc struct {
		Version        string           `json:"version"`
		Configurations []map[string]any `json:"configurations"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	c := doc.Configurations[0]
	if doc.Version != "0.2.0" || c["type"] != "go" || c["request"] != "attach" || c["mode"] != "remote" ||
		c["host"] != "127.0.0.1" || c["port"] != float64(2345) || c["apiVersion"] != float64(2) {
		t.Fatalf("%s", out)
	}

	// Existing configurations stay, ours is replaced by name and not duplicated.
	existing := []byte(`{"version":"0.2.0","configurations":[{"name":"mine","type":"node"},{"name":"Krate: attach to the Go host","port":1}],"extra":true}`)
	out, ok = MergeLaunchJSON(existing, []map[string]any{VSCodeGoAttach("127.0.0.1:4000")})
	if !ok {
		t.Fatal("merge")
	}
	doc.Configurations = nil
	var raw map[string]any
	_ = json.Unmarshal(out, &raw)
	_ = json.Unmarshal(out, &doc)
	if len(doc.Configurations) != 2 || doc.Configurations[0]["name"] != "mine" || doc.Configurations[1]["port"] != float64(4000) || raw["extra"] != true {
		t.Fatalf("%s", out)
	}

	// Comments cannot be preserved, so the file is left alone.
	if _, ok := MergeLaunchJSON([]byte("{\n  // my configs\n  \"configurations\": []\n}"), cfg); ok {
		t.Fatal("a launch.json with comments must not be rewritten")
	}
	if _, ok := MergeLaunchJSON([]byte(`{"configurations": 3}`), cfg); ok {
		t.Fatal("configurations that is not a list")
	}
}

func TestWriteVSCodeLaunch(t *testing.T) {
	root := t.TempDir()
	cfg := []map[string]any{VSCodeGoAttach("127.0.0.1:2345"), VSCodeNodeAttach("node", "127.0.0.1:9229")}
	path, written, err := WriteVSCodeLaunch(root, cfg)
	if err != nil || !written || path != filepath.Join(root, ".vscode", "launch.json") {
		t.Fatalf("%q %v %v", path, written, err)
	}
	if _, written, _ := WriteVSCodeLaunch(root, cfg); !written {
		t.Fatal("a second write merges")
	}
	b, _ := os.ReadFile(path)
	if strings.Count(string(b), "attach to the Go host") != 1 {
		t.Fatalf("duplicated:\n%s", b)
	}
	if err := os.WriteFile(path, []byte("// comment\n{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, written, err := WriteVSCodeLaunch(root, cfg); err != nil || written {
		t.Fatalf("a commented file is left alone: %v %v", written, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "// comment\n{}" {
		t.Fatalf("the file changed: %s", b)
	}
}
