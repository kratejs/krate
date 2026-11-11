package runtimes

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func TestCommand(t *testing.T) {
	cases := []struct {
		rt   Runtime
		want []string
	}{
		{Node, []string{"node", "--enable-source-maps", "{entry}"}},
		{Bun, []string{"bun", "run", "{entry}"}},
	}
	for _, c := range cases {
		if got := Command(c.rt, nil); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Command(%s) = %v", c.rt, got)
		}
	}
	deno := Command(Deno, []string{"net"})
	want := []string{"deno", "run", "--no-prompt", "--allow-env", "--allow-read={sockdir}", "--allow-write={sockdir}", "--allow-net", "{entry}"}
	if !reflect.DeepEqual(deno, want) {
		t.Errorf("deno command %v", deno)
	}
}

func TestDenoFlags(t *testing.T) {
	cases := []struct {
		name   string
		perms  []string
		scoped bool
		want   string
	}{
		{"none, scoped", nil, true, "--no-prompt --allow-env --allow-read={sockdir} --allow-write={sockdir} --allow-net=127.0.0.1"},
		{"none, compiled", nil, false, "--no-prompt --allow-env --allow-read --allow-write --allow-net=127.0.0.1"},
		{"fs", []string{"fs"}, true, "--no-prompt --allow-env --allow-read --allow-write --allow-net=127.0.0.1"},
		{"fs:read keeps write scoped", []string{"fs:read"}, true, "--no-prompt --allow-env --allow-read --allow-write={sockdir} --allow-net=127.0.0.1"},
		{"net and os", []string{"net", "os:info"}, true, "--no-prompt --allow-env --allow-read={sockdir} --allow-write={sockdir} --allow-net --allow-sys"},
		{"ui areas add nothing", []string{"window", "dialog", "kv", "db", "clipboard", "shell:openExternal", "os:secrets"}, true, "--no-prompt --allow-env --allow-read={sockdir} --allow-write={sockdir} --allow-net=127.0.0.1"},
	}
	for _, c := range cases {
		if got := strings.Join(DenoFlags(c.perms, c.scoped), " "); got != c.want {
			t.Errorf("%s: %s\nwant %s", c.name, got, c.want)
		}
	}
	for perm, flags := range DenoPermissionTable() {
		got := strings.Join(DenoFlags([]string{perm}, false), " ")
		for _, f := range flags {
			if !strings.Contains(got, f) {
				t.Errorf("%s: %q missing from %q", perm, f, got)
			}
		}
	}
}

func TestTargets(t *testing.T) {
	bun := map[Target]string{
		{"linux", "amd64"}: "bun-linux-x64", {"linux", "arm64"}: "bun-linux-arm64",
		{"darwin", "arm64"}: "bun-darwin-arm64", {"windows", "amd64"}: "bun-windows-x64",
	}
	for tg, want := range bun {
		if got, err := bunTarget(tg); err != nil || got != want {
			t.Errorf("bunTarget(%v) = %q, %v", tg, got, err)
		}
	}
	deno := map[Target]string{
		{"linux", "amd64"}: "x86_64-unknown-linux-gnu", {"darwin", "arm64"}: "aarch64-apple-darwin",
		{"windows", "amd64"}: "x86_64-pc-windows-msvc", {"linux", "arm64"}: "aarch64-unknown-linux-gnu",
	}
	for tg, want := range deno {
		if got, err := denoTarget(tg); err != nil || got != want {
			t.Errorf("denoTarget(%v) = %q, %v", tg, got, err)
		}
	}
	if _, err := bunTarget(Target{"plan9", "amd64"}); err == nil {
		t.Error("plan9 accepted")
	}
}

func TestReleaseArgs(t *testing.T) {
	bun, err := BunCompileArgs(Target{"linux", "arm64"}, "/w/host.mjs", "/o/host")
	if err != nil || strings.Join(bun, " ") != "build --compile --target=bun-linux-arm64 --outfile /o/host /w/host.mjs" {
		t.Errorf("bun args %v, %v", bun, err)
	}
	deno, err := DenoCompileArgs(Target{"darwin", "arm64"}, []string{"fs:read", "net"}, "/w/host.mjs", "/o/host")
	want := "compile --target aarch64-apple-darwin --output /o/host --no-prompt --allow-env --allow-read --allow-write --allow-net /w/host.mjs"
	if err != nil || strings.Join(deno, " ") != want {
		t.Errorf("deno args %v, %v", deno, err)
	}
	pj := PostjectArgs(Target{"linux", "amd64"}, "/o/node", "/w/sea.blob")
	if strings.Join(pj, " ") != "/o/node NODE_SEA_BLOB /w/sea.blob --sentinel-fuse "+SEAFuse {
		t.Errorf("postject args %v", pj)
	}
	pj = PostjectArgs(Target{"darwin", "arm64"}, "/o/node", "/w/sea.blob")
	if !strings.HasSuffix(strings.Join(pj, " "), "--macho-segment-name NODE_SEA") {
		t.Errorf("darwin postject args %v", pj)
	}
}

func TestVersionMatches(t *testing.T) {
	for _, c := range []struct {
		want, have string
		ok         bool
	}{
		{"", "22.1.0", true}, {"22", "22.1.0", true}, {"v22", "22.1.0", true}, {"22.1", "22.1.0", true},
		{"22.2", "22.1.0", false}, {"2", "22.1.0", false}, {"1.4", "1.4.2", true}, {"1.4.2", "1.4.2", true},
	} {
		if VersionMatches(c.want, c.have) != c.ok {
			t.Errorf("VersionMatches(%q, %q) != %v", c.want, c.have, c.ok)
		}
	}
}

type call struct {
	dir, name string
	env, args []string
}

// fakeTools replaces command execution and lookup. exists lists the tool names found on PATH.
func fakeTools(t *testing.T, exists map[string]string, versions map[string]string) *[]call {
	t.Helper()
	var calls []call
	oldRun, oldLook, oldEnv := run, lookPath, envGet
	t.Cleanup(func() { run, lookPath, envGet = oldRun, oldLook, oldEnv })
	run = func(dir string, env []string, name string, args ...string) ([]byte, error) {
		if len(args) == 1 && args[0] == "--version" {
			if v, ok := versions[name]; ok {
				return []byte(v + "\n"), nil
			}
			return nil, errors.New("no such tool")
		}
		calls = append(calls, call{dir, name, env, args})
		// The node blob step writes a file the later steps read.
		for i, a := range args {
			if a == "--experimental-sea-config" {
				_ = os.WriteFile(filepath.Join(dir, "sea.blob"), []byte("blob"), 0o644)
				_ = i
			}
		}
		return nil, nil
	}
	lookPath = func(name string) (string, error) {
		if p, ok := exists[name]; ok {
			return p, nil
		}
		return "", errors.New("not found")
	}
	envGet = func(string) string { return "" }
	return &calls
}

func writeEntry(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "host.mjs")
	if err := os.WriteFile(p, []byte(`import { readFileSync } from "node:fs"; console.log(typeof readFileSync);`), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBuildReleaseRequiresBundle(t *testing.T) {
	_, err := BuildRelease(Node, false, HostTarget(), t.TempDir(), ReleaseInput{Entry: writeEntry(t)})
	var d diag.Diagnostic
	if !errors.As(err, &d) || d.Code != diag.CodeRuntimeUnavailable || !strings.Contains(d.Message, "native.host.bundle") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildReleaseBun(t *testing.T) {
	calls := fakeTools(t, map[string]string{"bun": "/usr/bin/bun"}, map[string]string{"/usr/bin/bun": "1.4.2"})
	out := t.TempDir()
	exe, err := BuildRelease(Bun, true, Target{"linux", "arm64"}, out, ReleaseInput{Entry: writeEntry(t), Version: "1.4", Name: "host-bun"})
	if err != nil {
		t.Fatal(err)
	}
	if exe != filepath.Join(out, "host-bun") {
		t.Errorf("exe %s", exe)
	}
	if len(*calls) != 1 || (*calls)[0].name != "/usr/bin/bun" || (*calls)[0].args[2] != "--target=bun-linux-arm64" {
		t.Errorf("calls %+v", *calls)
	}
	_, err = BuildRelease(Bun, true, HostTarget(), out, ReleaseInput{Entry: writeEntry(t), Version: "2"})
	var d diag.Diagnostic
	if !errors.As(err, &d) || d.Code != diag.CodeRuntimeUnavailable || !strings.Contains(d.Message, "1.4.2") {
		t.Errorf("version mismatch err = %v", err)
	}
}

func TestBuildReleaseDeno(t *testing.T) {
	calls := fakeTools(t, map[string]string{"deno": "/usr/bin/deno"}, map[string]string{"/usr/bin/deno": "deno 2.1.4 (stable, release, x86_64-unknown-linux-gnu)"})
	out := t.TempDir()
	exe, err := BuildRelease(Deno, true, Target{"windows", "amd64"}, out, ReleaseInput{Entry: writeEntry(t), Permissions: []string{"net"}})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(exe) != "krate-host-deno.exe" {
		t.Errorf("exe %s", exe)
	}
	joined := strings.Join((*calls)[0].args, " ")
	if !strings.Contains(joined, "--target x86_64-pc-windows-msvc") || !strings.Contains(joined, "--allow-net") {
		t.Errorf("deno args %s", joined)
	}
}

func TestBuildReleaseNodeSEA(t *testing.T) {
	calls := fakeTools(t, map[string]string{"node": "/usr/bin/node", "postject": "/usr/bin/postject"}, map[string]string{"/usr/bin/node": "v22.5.1"})
	// The fake node binary is copied to become the executable.
	node := filepath.Join(t.TempDir(), "node")
	if err := os.WriteFile(node, []byte("node-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	lookPath = func(name string) (string, error) {
		switch name {
		case "node":
			return node, nil
		case "postject":
			return "/usr/bin/postject", nil
		}
		return "", errors.New("not found")
	}
	run0 := run
	run = func(dir string, env []string, name string, args ...string) ([]byte, error) {
		if len(args) == 1 && args[0] == "--version" {
			return []byte("v22.5.1\n"), nil
		}
		return run0(dir, env, name, args...)
	}
	out := t.TempDir()
	exe, err := BuildRelease(Node, true, HostTarget(), out, ReleaseInput{Entry: writeEntry(t), Version: "22"})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "node-binary" {
		t.Errorf("executable is not a copy of node")
	}
	if len(*calls) != 2 {
		t.Fatalf("calls %+v", *calls)
	}
	if (*calls)[0].args[0] != "--experimental-sea-config" {
		t.Errorf("first call %+v", (*calls)[0])
	}
	pj := (*calls)[1]
	if pj.name != "/usr/bin/postject" || pj.args[1] != "NODE_SEA_BLOB" || pj.args[len(pj.args)-1] != SEAFuse {
		t.Errorf("postject call %+v", pj)
	}
	if _, err := os.Stat(filepath.Join(out, ".sea-krate-host-node")); !os.IsNotExist(err) {
		t.Errorf("scratch directory left behind")
	}
	// A foreign target needs that platform's binary.
	_, err = BuildRelease(Node, true, Target{"windows", "arm64"}, out, ReleaseInput{Entry: writeEntry(t)})
	var d diag.Diagnostic
	if !errors.As(err, &d) || !strings.Contains(d.Hint, "KRATE_NODE_WINDOWS_ARM64") {
		t.Errorf("cross err = %v", err)
	}
}

func TestBuildReleaseMissingRuntime(t *testing.T) {
	fakeTools(t, nil, nil)
	_, err := BuildRelease(Bun, true, HostTarget(), t.TempDir(), ReleaseInput{Entry: writeEntry(t)})
	var d diag.Diagnostic
	if !errors.As(err, &d) || d.Code != diag.CodeRuntimeUnavailable || !strings.Contains(d.Hint, "KRATE_BUN") {
		t.Fatalf("err = %v", err)
	}
}
