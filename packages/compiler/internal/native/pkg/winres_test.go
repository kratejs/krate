package pkg

import (
	"debug/pe"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func goBuild(t *testing.T, dir, goos, goarch, out string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0", "GOWORK=off", "GOFLAGS=-mod=mod")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s/%s: %v\n%s", goos, goarch, err, b)
	}
}

func tinyProgram(t *testing.T) string {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module tiny\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// readResources walks the resource directory of a PE file.
func readResources(t *testing.T, path string) map[uint32][]byte {
	t.Helper()
	f, err := pe.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var sec *pe.Section
	for _, s := range f.Sections {
		if s.Name == ".rsrc" {
			sec = s
		}
	}
	if sec == nil {
		t.Fatal("no .rsrc section")
	}
	data, err := sec.Data()
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	out := map[uint32][]byte{} // type<<16|name -> data
	var walk func(off uint32, depth int, typ, name uint32)
	walk = func(off uint32, depth int, typ, name uint32) {
		n := int(le.Uint16(data[off+12:])) + int(le.Uint16(data[off+14:]))
		for i := 0; i < n; i++ {
			e := off + 16 + uint32(8*i)
			id := le.Uint32(data[e:])
			to := le.Uint32(data[e+4:])
			if to&0x80000000 != 0 {
				switch depth {
				case 0:
					walk(to&0x7fffffff, 1, id, 0)
				case 1:
					walk(to&0x7fffffff, 2, typ, id)
				}
				continue
			}
			rva := le.Uint32(data[to:])
			size := le.Uint32(data[to+4:])
			start := rva - sec.VirtualAddress
			out[typ<<16|name] = data[start : start+size]
		}
	}
	walk(0, 0, 0, 0)
	return out
}

func utf16Contains(b []byte, s string) bool {
	u := utf16.Encode([]rune(s))
	pat := make([]byte, 0, 2*len(u))
	for _, c := range u {
		pat = append(pat, byte(c), byte(c>>8))
	}
	return strings.Contains(string(b), string(pat))
}

func TestWindowsResourcesLinkAndParse(t *testing.T) {
	if testing.Short() {
		t.Skip("builds Go programs")
	}
	root := t.TempDir()
	iconPath := writeTestIcon(t, 512, 512)
	cfg := &config.Config{Native: &config.NativeConfig{
		AppID: "com.acme.notes", ProductName: "Acme Notes", Version: "1.2.3", Icons: iconPath,
		Bundle: config.NativeBundle{Windows: config.NativeBundleWindows{Publisher: "CN=Acme Inc"}},
	}}
	for _, arch := range []string{"amd64", "arm64"} {
		dir := tinyProgram(t)
		syso := filepath.Join(dir, "rsrc_windows_"+arch+".syso")
		if err := WindowsResources(root, cfg, syso); err != nil {
			t.Fatal(err)
		}
		exe := filepath.Join(dir, "tiny-"+arch+".exe")
		goBuild(t, dir, "windows", arch, exe)
		res := readResources(t, exe)
		group := res[rtGroupIcon<<16|1]
		if len(group) != 6+14*len(icoSizes) || binary.LittleEndian.Uint16(group[2:]) != 1 {
			t.Fatalf("%s: group icon %d bytes", arch, len(group))
		}
		for i := range icoSizes {
			e := group[6+14*i:]
			id := binary.LittleEndian.Uint16(e[12:])
			size := binary.LittleEndian.Uint32(e[8:])
			img := res[rtIcon<<16|uint32(id)]
			if uint32(len(img)) != size || string(img[1:4]) != "PNG" {
				t.Errorf("%s: icon %d is not the PNG the group names", arch, id)
			}
		}
		ver := res[rtVersion<<16|1]
		if binary.LittleEndian.Uint32(ver[40:]) != 0xFEEF04BD {
			t.Fatalf("%s: VS_FIXEDFILEINFO signature missing", arch)
		}
		if ms := binary.LittleEndian.Uint32(ver[48:]); ms != 1<<16|2 {
			t.Errorf("%s: FileVersionMS %x", arch, ms)
		}
		if int(binary.LittleEndian.Uint16(ver)) != len(ver) {
			t.Errorf("%s: version block length %d, resource %d", arch, binary.LittleEndian.Uint16(ver), len(ver))
		}
		for _, want := range []string{"VS_VERSION_INFO", "StringFileInfo", "040904B0", "CompanyName", "Acme Inc", "ProductName", "Acme Notes", "Acme Notes.exe", "Translation"} {
			if !utf16Contains(ver, want) {
				t.Errorf("%s: version info lacks %q", arch, want)
			}
		}
		man := string(res[rtManifest<<16|1])
		for _, want := range []string{"PerMonitorV2", "Microsoft.Windows.Common-Controls", `version="6.0.0.0"`, "com.acme.notes", "longPathAware"} {
			if !strings.Contains(man, want) {
				t.Errorf("%s: manifest lacks %q", arch, want)
			}
		}
	}
}
