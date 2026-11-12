package build

import (
	"debug/macho"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/native/pkg"
)

// The example is compiled for darwin/arm64 and darwin/amd64 (Go cross compiles
// both from any OS), merged into one universal .app, and that app's executable
// is a fat Mach-O with both slices. lipo, when installed, agrees.
func TestNativeUniversalMacApp(t *testing.T) {
	requireRelease(t)
	ex := e2eRequireDeps(t)
	out := filepath.Join(t.TempDir(), "dist-native")
	cfg, err := config.Load(ex)
	if err != nil {
		t.Fatal(err)
	}
	b := New(ex, cfg)
	res, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: out, Targets: []string{"darwin/arm64", "darwin/amd64"}, ExtraTags: []string{"krate_devcontrol"}})
	if err != nil {
		t.Fatalf("release build: %v", err)
	}
	if !res.Compiled || len(res.Binaries) != 2 {
		t.Fatalf("release build produced %+v", res)
	}
	meta := pkg.MetadataFromConfig(ex, cfg)
	meta.Permissions = res.Manifest.Permissions
	payload, err := pkg.DiscoverUniversal(out, meta.ProductName, filepath.Join(out, ".universal"))
	if err != nil {
		t.Fatal(err)
	}
	pk, err := pkg.Package(pkg.Options{Meta: meta, Payload: payload, OutDir: filepath.Join(out, "packages")})
	if err != nil {
		t.Fatalf("packaging: %v", err)
	}
	var app, zip string
	for _, a := range pk.Artifacts {
		switch a.Format {
		case "app":
			app = a.Path
		case "zip":
			zip = a.Path
		}
	}
	if !strings.HasSuffix(zip, "-darwin-universal.zip") {
		t.Errorf("update file %q", zip)
	}
	exe := filepath.Join(app, "Contents", "MacOS", meta.ProductName)
	archs, err := pkg.FatArchitectures(exe)
	if err != nil || strings.Join(archs, ",") != "amd64,arm64" {
		t.Fatalf("architectures = %v, %v", archs, err)
	}
	fat, err := macho.OpenFat(exe)
	if err != nil {
		t.Fatal(err)
	}
	fat.Close()
	for _, n := range pk.Notes {
		if strings.Contains(n, "no code signature") {
			t.Errorf("the Go linker signs the arm64 slice: %s", n)
		}
	}
	if lipo, err := exec.LookPath("lipo"); err == nil {
		o, err := exec.Command(lipo, "-info", exe).CombinedOutput()
		s := string(o)
		if err != nil || !strings.Contains(s, "x86_64") || !strings.Contains(s, "arm64") || !strings.Contains(s, "Architectures in the fat file") {
			t.Errorf("lipo -info: %v\n%s", err, s)
		}
		if o, err := exec.Command(lipo, "-verify_arch", exe, "x86_64", "arm64").CombinedOutput(); err != nil {
			t.Errorf("lipo -verify_arch: %v\n%s", err, o)
		}
	}
	if runtime.GOOS != "darwin" || os.Getenv("KRATE_NATIVE_GUI") != "1" {
		return
	}
	// Both slices start: the native one, and the other under Rosetta when it
	// is installed. A wrapper script picks the architecture because
	// e2eStartShell passes its own arguments first.
	for _, arch := range []string{"arm64", "x86_64"} {
		if exec.Command("arch", "-"+arch, "/usr/bin/true").Run() != nil {
			t.Logf("skipping the %s slice: this machine cannot run it", arch)
			continue
		}
		wrapper := filepath.Join(t.TempDir(), "run-"+arch+".sh")
		script := "#!/bin/sh\nexec arch -" + arch + " \"" + exe + "\" \"$@\"\n"
		if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		s := e2eStartShell(t, wrapper, "")
		s.waitReady(90 * time.Second)
		if _, err := s.send("quit", 10*time.Second); err != nil {
			t.Errorf("%s: quit: %v", arch, err)
		}
	}
}
