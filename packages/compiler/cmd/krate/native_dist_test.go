package main

import (
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func distCfg(universal bool) *config.Config {
	c := config.Default()
	c.Native = &config.NativeConfig{AppID: "com.acme.app"}
	c.Native.Bundle.MacOS.Universal = universal
	return c
}

func TestResolveDistTargets(t *testing.T) {
	flags := cliFlags{}
	flags.Pkg.Arch = "universal"
	flags.Targets = []string{"darwin/arm64"}
	if err := resolveDistTargets(&flags, distCfg(false)); err != nil {
		t.Fatal(err)
	}
	if flags.Target != "darwin/universal" || strings.Join(buildTargets(flags.Targets), " ") != "darwin/arm64 darwin/amd64" || !hasUniversal(flags.Targets) {
		t.Errorf("flags = %+v", flags)
	}

	flags = cliFlags{}
	flags.Pkg.Arch = "universal"
	flags.Targets = []string{"linux/amd64"}
	if err := resolveDistTargets(&flags, distCfg(false)); err == nil || !strings.Contains(err.Error(), "applies to macOS") {
		t.Errorf("linux universal: %v", err)
	}

	flags = cliFlags{Targets: []string{"mas"}}
	if err := resolveDistTargets(&flags, distCfg(false)); err != nil {
		t.Fatal(err)
	}
	if flags.Target != "darwin/"+runtime.GOARCH || !flags.Pkg.MAS || strings.Join(flags.Pkg.Formats, ",") != "mas" {
		t.Errorf("mas = %+v", flags)
	}
	flags = cliFlags{Targets: []string{"mas"}}
	if err := resolveDistTargets(&flags, distCfg(true)); err != nil || flags.Target != "darwin/universal" {
		t.Errorf("mas with config universal: %v %+v", err, flags)
	}
	flags = cliFlags{Targets: []string{"mas/arm64"}, Pkg: nativePkgFlags{Formats: []string{"mas"}}}
	if err := resolveDistTargets(&flags, distCfg(false)); err != nil || flags.Target != "darwin/arm64" {
		t.Errorf("mas/arm64: %v %+v", err, flags)
	}

	// An explicit --target beats the config key.
	flags = cliFlags{Targets: []string{"darwin/arm64"}}
	if err := resolveDistTargets(&flags, distCfg(true)); err != nil || flags.Target != "darwin/arm64" {
		t.Errorf("explicit target: %v %+v", err, flags)
	}

	flags = cliFlags{Targets: []string{"windows/universal"}, Pkg: nativePkgFlags{Formats: []string{"nsis"}}}
	if err := resolveDistTargets(&flags, distCfg(false)); err == nil || !strings.Contains(err.Error(), "msix-store") {
		t.Errorf("windows universal formats: %v", err)
	}
	flags = cliFlags{Targets: []string{"windows/universal"}}
	if err := resolveDistTargets(&flags, distCfg(false)); err != nil || strings.Join(flags.Pkg.Formats, ",") != "msix-store" {
		t.Errorf("windows universal: %v %+v", err, flags)
	}
	if strings.Join(buildTargets(flags.Targets), " ") != "windows/amd64 windows/arm64" {
		t.Errorf("windows build targets: %v", buildTargets(flags.Targets))
	}
}

func TestArchFlag(t *testing.T) {
	f, _, err := parseFlagsChecked([]string{"native", "package", ".", "--arch", "universal", "--installer-identity=Inst"})
	if err != nil || f.Pkg.Arch != "universal" || f.Pkg.InstallerIdentity != "Inst" {
		t.Errorf("%v %+v", err, f.Pkg)
	}
	if _, _, err := parseFlagsChecked([]string{"native", "package", "--arch", "arm64"}); err == nil || !strings.Contains(err.Error(), "universal") {
		t.Errorf("--arch arm64: %v", err)
	}
	if o, a, err := nativeHostTarget("darwin/universal"); err != nil || o != "darwin" || a != "universal" {
		t.Errorf("nativeHostTarget: %s %s %v", o, a, err)
	}
	if _, _, err := nativeHostTarget("linux/universal"); err == nil {
		t.Error("linux/universal is not a target")
	}
}
