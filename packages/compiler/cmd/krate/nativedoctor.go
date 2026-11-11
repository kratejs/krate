package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/build"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/native"
	"github.com/kratejs/krate/packages/compiler/internal/native/doctor"
)

// runNativeDoctor implements `krate native doctor`: it reports the Go
// toolchain, the shell binary, the system webview, the packaging and signing
// tools and the host runtime, and exits 1 when something the project's config
// needs is missing.
func runNativeDoctor(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	in := doctorInputs(root, cfg)
	rep := doctor.Run(in, systemProbes(root, flags.Shell))
	if flags.JSON {
		data, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(data))
	} else {
		doctor.Format(os.Stdout, rep)
	}
	if len(rep.Failed()) > 0 {
		exit(1)
	}
}

// doctorInputs reads what the config and the code decide: whether there is Go
// host code, the release mode, the host runtime and the packaging choices.
func doctorInputs(root string, cfg *config.Config) doctor.Inputs {
	n := cfg.EffectiveNative()
	if n == nil {
		n = &config.NativeConfig{Release: config.NativeRelease{Compile: "auto"}, Host: config.NativeHost{Runtime: "quickjs"}}
	}
	in := doctor.Inputs{
		Compile: n.Release.Compile, HostRuntime: n.Host.Runtime, HostVersion: n.Host.Version,
		LinuxFormats: n.Bundle.Linux.Formats, WindowsInstaller: n.Bundle.Windows.Installer,
	}
	for _, rt := range n.Host.Islands {
		in.Islands = append(in.Islands, rt)
	}
	if p, err := native.Analyze(root, cfg); err == nil {
		for _, m := range p.Modules {
			if m.Source == native.SourceGo {
				in.GoHost = true
			}
		}
	}
	return in
}

// systemProbes are the probes of this machine.
func systemProbes(root, shellFlag string) doctor.Probes {
	return doctor.Probes{
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Getenv:   os.Getenv,
		LookPath: exec.LookPath,
		Output: func(name string, args ...string) (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
			return string(out), err
		},
		FindShell: func() (string, string, error) {
			p, src, err := build.FindPrebuiltShell(shellFlag, runtime.GOOS, runtime.GOARCH)
			if err != nil || p != "" {
				return p, src, err
			}
			if cached := build.CachedMonorepoShell(root); cached != "" {
				return cached, "built from the checkout", nil
			}
			return "", "", nil
		},
		ShellDoctor: func(shell string) (*doctor.ShellInfo, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, shell, "--doctor")
			var stderr strings.Builder
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				if s := strings.TrimSpace(stderr.String()); s != "" {
					return nil, fmt.Errorf("%v: %s", err, s)
				}
				return nil, err
			}
			return doctor.ParseShellInfo(out)
		},
	}
}
