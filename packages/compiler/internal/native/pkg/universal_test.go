package pkg

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/updater"
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

func TestWriteFatLayout(t *testing.T) {
	d := t.TempDir()
	arm := writeThin(t, d, "arm", macho.CpuArm64, "ARM-SLICE")
	x86 := writeThin(t, d, "x86", macho.CpuAmd64, "X86-SLICE-LONGER")
	out := filepath.Join(d, "fat")
	if err := WriteFat(out, []string{arm, x86}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(out)
	if binary.BigEndian.Uint32(raw[:4]) != 0xcafebabe || binary.BigEndian.Uint32(raw[4:8]) != 2 {
		t.Fatalf("bad fat header % x", raw[:8])
	}
	// First entry is x86_64 (lipo order), second arm64; alignment 2^14.
	wantCPU := []uint32{uint32(macho.CpuAmd64), uint32(macho.CpuArm64)}
	for i := 0; i < 2; i++ {
		e := raw[8+20*i : 8+20*(i+1)]
		cpu, off, size, align := binary.BigEndian.Uint32(e[0:]), binary.BigEndian.Uint32(e[8:]), binary.BigEndian.Uint32(e[12:]), binary.BigEndian.Uint32(e[16:])
		if cpu != wantCPU[i] || align != 14 || off%(1<<14) != 0 {
			t.Errorf("entry %d: cpu=%x off=%d align=%d", i, cpu, off, align)
		}
		src := arm
		if i == 0 {
			src = x86
		}
		want, _ := os.ReadFile(src)
		if !bytes.Equal(raw[off:off+size], want) {
			t.Errorf("slice %d content differs", i)
		}
	}
	fat, err := macho.OpenFat(out)
	if err != nil {
		t.Fatalf("debug/macho cannot read the fat file: %v", err)
	}
	defer fat.Close()
	if len(fat.Arches) != 2 || fat.Arches[0].Cpu != macho.CpuAmd64 || fat.Arches[1].Cpu != macho.CpuArm64 {
		t.Errorf("arches = %+v", fat.Arches)
	}
	archs, err := FatArchitectures(out)
	if err != nil || strings.Join(archs, ",") != "amd64,arm64" {
		t.Errorf("FatArchitectures = %v, %v", archs, err)
	}
	if goos, goarch, err := BinaryTarget(out); err != nil || goos != "darwin" || goarch != "universal" {
		t.Errorf("BinaryTarget = %s %s %v", goos, goarch, err)
	}
	if st, _ := os.Stat(out); st.Mode().Perm()&0o111 == 0 {
		t.Error("the universal binary must be executable")
	}
}

func TestWriteFatRejects(t *testing.T) {
	d := t.TempDir()
	arm := writeThin(t, d, "arm", macho.CpuArm64, "a")
	arm2 := writeThin(t, d, "arm2", macho.CpuArm64, "b")
	if err := WriteFat(filepath.Join(d, "o"), []string{arm}); err == nil {
		t.Error("one slice is not universal")
	}
	if err := WriteFat(filepath.Join(d, "o"), []string{arm, arm2}); err == nil || !strings.Contains(err.Error(), "same architecture") {
		t.Errorf("same arch: %v", err)
	}
	txt := filepath.Join(d, "t.txt")
	_ = os.WriteFile(txt, []byte("not macho at all"), 0o644)
	if err := WriteFat(filepath.Join(d, "o"), []string{arm, txt}); err == nil || !strings.Contains(err.Error(), "not a Mach-O") {
		t.Errorf("text: %v", err)
	}
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

func TestMergeUniversalMergesMachOAndChecksTheRest(t *testing.T) {
	arm := archPayload(t, macho.CpuArm64, true, "<h1>x</h1>")
	amd := archPayload(t, macho.CpuAmd64, true, "<h1>x</h1>")
	work := filepath.Join(t.TempDir(), "u")
	p, err := MergeUniversal(arm, amd, work)
	if err != nil {
		t.Fatal(err)
	}
	if p.Arch != ArchUniversal || p.OS != "darwin" {
		t.Errorf("payload = %+v", p)
	}
	for _, f := range []string{p.Executable, filepath.Join(p.ExtraFiles["host"], "krate-host-node")} {
		a, err := FatArchitectures(f)
		if err != nil || strings.Join(a, ",") != "amd64,arm64" {
			t.Errorf("%s: %v %v", f, a, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(p.ExtraFiles["app"], "dist", "index.html")); string(b) != "<h1>x</h1>" {
		t.Errorf("shared file lost: %q", b)
	}
	if l, err := os.Readlink(filepath.Join(p.ExtraFiles["app"], "dist", "home.html")); err != nil || l != "index.html" {
		t.Errorf("symlink = %q %v", l, err)
	}
	if err := checkTarget(p); err != nil {
		t.Errorf("a universal executable must pass the target check: %v", err)
	}
}

func TestMergeUniversalDiagnostics(t *testing.T) {
	work := func() string { return filepath.Join(t.TempDir(), "u") }
	arm := archPayload(t, macho.CpuArm64, true, "a")
	amd := archPayload(t, macho.CpuAmd64, false, "a")
	if _, err := MergeUniversal(arm, amd, work()); err == nil || !strings.Contains(err.Error(), "KR7300") || !strings.Contains(err.Error(), `"host" exists in the arm64 build`) {
		t.Errorf("missing host child: %v", err)
	}
	amd = archPayload(t, macho.CpuAmd64, true, "DIFFERENT")
	if _, err := MergeUniversal(arm, amd, work()); err == nil || !strings.Contains(err.Error(), "index.html") {
		t.Errorf("differing UI file: %v", err)
	}
	// An arm64 executable in the amd64 slot.
	if _, err := MergeUniversal(arm, arm, work()); err == nil || !strings.Contains(err.Error(), "not arm64 or") {
		t.Errorf("wrong arch: %v", err)
	}
}

func TestDiscoverUniversalNeedsBothBuilds(t *testing.T) {
	out := t.TempDir()
	arm := archPayload(t, macho.CpuArm64, false, "a")
	dir := filepath.Join(out, "darwin-arm64")
	_ = os.MkdirAll(dir, 0o755)
	data, _ := os.ReadFile(arm.Executable)
	_ = os.WriteFile(filepath.Join(dir, "Acme Notes"), data, 0o755)
	_, err := DiscoverUniversal(out, "Acme Notes", filepath.Join(out, "w"))
	if err == nil || !strings.Contains(err.Error(), "darwin/amd64") || !strings.Contains(err.Error(), "--arch universal") {
		t.Errorf("err = %v", err)
	}
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

func TestUniversalUpdateFeedTarget(t *testing.T) {
	arm := archPayload(t, macho.CpuArm64, false, "a")
	amd := archPayload(t, macho.CpuAmd64, false, "a")
	pl, _ := MergeUniversal(arm, amd, filepath.Join(t.TempDir(), "u"))
	m := testMeta(t)
	seed, _, _ := updater.GenerateKey()
	priv, _ := updater.PrivateKeyFromSeed(seed)
	res := run(t, Options{Meta: m, Payload: pl, UpdaterKey: priv, Runner: &fakeRunner{goos: "linux"}})
	feed := find(t, res, "update-feed")
	if filepath.Base(feed.Path) != "latest-darwin-universal.json" {
		t.Errorf("feed = %s", feed.Path)
	}
}

func TestPackageUniversalNotesUnsignedArmSlice(t *testing.T) {
	arm := archPayload(t, macho.CpuArm64, false, "a")
	amd := archPayload(t, macho.CpuAmd64, false, "a")
	pl, _ := MergeUniversal(arm, amd, filepath.Join(t.TempDir(), "u"))
	res := run(t, Options{Meta: testMeta(t), Payload: pl})
	found := false
	for _, n := range res.Notes {
		found = found || strings.Contains(n, "no code signature")
	}
	if !found {
		t.Errorf("an unsigned arm64 slice must be reported: %v", res.Notes)
	}
}
