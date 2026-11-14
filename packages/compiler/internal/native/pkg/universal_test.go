package pkg

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// thinMachO builds a minimal 64-bit Mach-O executable header for cpu with
// payload bytes after it. debug/macho parses it (no load commands).
func thinMachO(cpu macho.Cpu, sub uint32, payload string) []byte {
	var b bytes.Buffer
	for _, v := range []uint32{0xfeedfacf, uint32(cpu), sub, 2 /* MH_EXECUTE */, 0, 0, 0, 0} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString(payload)
	return b.Bytes()
}

func writeThin(t *testing.T, dir, name string, cpu macho.Cpu, payload string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	sub := uint32(0)
	if cpu == macho.CpuArm64 {
		sub = 0
	} else {
		sub = 3
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, thinMachO(cpu, sub, payload), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// archPayload makes a payload for one architecture: the executable, a host
// child Mach-O, shared UI files and a symlink.
func archPayload(t *testing.T, cpu macho.Cpu, child bool, ui string) Payload {
	t.Helper()
	d := t.TempDir()
	exe := writeThin(t, d, "Acme Notes", cpu, "main")
	app := filepath.Join(d, "app")
	_ = os.MkdirAll(filepath.Join(app, "dist"), 0o755)
	_ = os.WriteFile(filepath.Join(app, "app.json"), []byte(`{"appId":"com.acme.notes"}`), 0o644)
	_ = os.WriteFile(filepath.Join(app, "dist", "index.html"), []byte(ui), 0o644)
	_ = os.Symlink("index.html", filepath.Join(app, "dist", "home.html"))
	extra := map[string]string{"app": app}
	if child {
		extra["host"] = filepath.Dir(writeThin(t, d, "host/krate-host-node", cpu, "node-"+cpu.String()))
	}
	return Payload{OS: "darwin", Arch: map[macho.Cpu]string{macho.CpuArm64: "arm64", macho.CpuAmd64: "amd64"}[cpu], Executable: exe, ExtraFiles: extra}
}

func TestPackageUniversalMacApp(t *testing.T) {
	arm := archPayload(t, macho.CpuArm64, false, "a")
	amd := archPayload(t, macho.CpuAmd64, false, "a")
	pl, err := MergeUniversal(arm, amd, filepath.Join(t.TempDir(), "u"))
	if err != nil {
		t.Fatal(err)
	}
	m := testMeta(t)
	res := run(t, Options{Meta: m, Payload: pl, Runner: &fakeRunner{goos: "linux"}})
	zipA := find(t, res, "zip")
	if !strings.HasSuffix(zipA.Path, "acme-notes-1.2.0-darwin-universal.zip") {
		t.Errorf("zip name %s", zipA.Path)
	}
	app := find(t, res, "app").Path
	if !strings.HasSuffix(filepath.Dir(app), "darwin-universal") {
		t.Errorf("stage %s", app)
	}
	b, err := os.ReadFile(filepath.Join(app, "Contents", "Resources", "krate-dist.json"))
	if err != nil || !strings.Contains(string(b), `"target": "darwin-universal"`) {
		t.Errorf("marker = %q %v", b, err)
	}
	archs, err := FatArchitectures(filepath.Join(app, "Contents", "MacOS", "Acme Notes"))
	if err != nil || len(archs) != 2 {
		t.Errorf("archs = %v %v", archs, err)
	}
	_ = res
}
