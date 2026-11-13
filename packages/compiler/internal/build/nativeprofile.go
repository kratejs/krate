package build

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// NativeProfileOptions tune ProfileNative.
type NativeProfileOptions struct {
	// OutDir is the native output directory (default dist-native).
	OutDir string
	// Out is where the profile is written; default native.release.pgo
	// (native.pgo) relative to the project root.
	Out string
	// Shell is the shell binary for the prebuilt-shell layout.
	Shell string
	// Args are passed to the app (for example --browser).
	Args []string
	// Stdout and Stderr receive the app's output.
	Stdout, Stderr io.Writer
	// Ctx, when done, asks the app to exit (interrupt, then kill after 10 s).
	Ctx context.Context
	// OnStart is called once the app runs, with its process.
	OnStart func(*os.Process)
}

// NativeProfileResult describes a collected profile.
type NativeProfileResult struct {
	// Path is the absolute path of the profile (native.release.pgo).
	Path string
	Size int64
	// Binary is the program that ran.
	Binary string
}

// ProfileNative builds the app (a release executable when Go is available,
// the prebuilt-shell layout otherwise), runs it with KRATE_CPUPROFILE set until
// it exits, and copies the CPU profile to native.release.pgo.
func (b *Builder) ProfileNative(opts NativeProfileOptions) (*NativeProfileResult, error) {
	fail := func(msg, hint string) error {
		return nativeDiagError([]diag.Diagnostic{diag.New("native.pgo", 1, 1, msg, hint, "").WithCode(diag.CodeNativeProfile)}, "native profile error(s)")
	}
	eff := nativeEffective(b.Cfg)
	dest := opts.Out
	if dest == "" {
		dest = eff.Release.PGO
	}
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(b.Root, dest)
	}

	rel, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: opts.OutDir})
	if err != nil {
		return nil, err
	}
	var cmd *exec.Cmd
	var binary string
	if rel.Compiled {
		binary = rel.Binaries[0].Path
		cmd = exec.Command(binary, opts.Args...)
	} else {
		shell, err := LocateNativeShell(b.Root, opts.Shell)
		if err != nil {
			return nil, fail("no program to profile: "+err.Error(), "install Go to build a release executable, or pass --shell <path>")
		}
		binary = shell
		cmd = exec.Command(shell, append(append([]string(nil), opts.Args...), rel.AppDir)...)
	}
	tmp := filepath.Join(b.nativeOutDir(), "profile.pprof")
	_ = os.Remove(tmp)
	if err := os.MkdirAll(filepath.Dir(tmp), 0o755); err != nil {
		return nil, err
	}
	cmd.Env = append(os.Environ(), "KRATE_CPUPROFILE="+tmp)
	cmd.Stdout, cmd.Stderr = opts.Stdout, opts.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return nil, fail("cannot start "+binary+": "+err.Error(), "run `krate native build --release` first and check the executable runs")
	}
	if opts.OnStart != nil {
		opts.OnStart(cmd.Process)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case waitErr = <-done:
	case <-ctx.Done():
		stopProcess(cmd.Process)
		select {
		case waitErr = <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			waitErr = <-done
		}
	}
	_ = waitErr // a signal exit is the normal way to end a profiling run
	data, err := os.ReadFile(tmp)
	if err != nil || len(data) == 0 {
		return nil, fail("the app wrote no profile",
			"the app must exit normally for the profile to be written: close its window or press Ctrl-C (a killed process writes nothing)")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return nil, fail("cannot write "+dest+": "+err.Error(), "check the directory is writable")
	}
	return &NativeProfileResult{Path: dest, Size: int64(len(data)), Binary: binary}, nil
}

// stopProcess asks p to exit the way Ctrl-C does.
func stopProcess(p *os.Process) {
	if runtime.GOOS == "windows" {
		_ = p.Kill()
		return
	}
	_ = p.Signal(os.Interrupt)
}
