package runtimes

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// ReleaseInput is what BuildRelease needs besides the runtime and target.
type ReleaseInput struct {
	// Entry is the absolute path of the host child bundle (an ES module that
	// imports nothing but node: built-ins), as written by the host build under
	// .krate/native/host/<runtime>/host.mjs.
	Entry string
	// Name is the executable's base name, without extension. Default
	// "krate-host-<runtime>".
	Name string
	// Version is native.host.version: the major (or major.minor) version the
	// runtime must have. Empty accepts any.
	Version string
	// Permissions is native.permissions; Deno maps it to --allow-* flags.
	Permissions []string
}

// BuildRelease turns a host child bundle into a self-contained executable for
// target in outDir and returns its path. The executable speaks the host child
// protocol and needs nothing installed:
//
//	node     Node single executable application: a copy of the node binary with the
//	         bundle injected as a SEA blob
//	bun      bun build --compile --target=bun-<os>-<arch>
//	deno     deno compile --target <triple>, native.permissions as --allow-* flags
//	scriptc  scriptc build (a native executable with no JavaScript engine)
//
// bundle is native.host.bundle. A runtime found on PATH is for development
// only, so bundle false is an error here (KR7233).
//
// The runtime that does the compiling is taken from KRATE_NODE, KRATE_BUN,
// KRATE_DENO or PATH. A node build for another operating system or
// architecture needs that target's node binary in KRATE_NODE_<OS>_<ARCH>, for
// example KRATE_NODE_LINUX_ARM64.
func BuildRelease(rt Runtime, bundle bool, target Target, outDir string, in ReleaseInput) (string, error) {
	if !bundle {
		return "", unavailable(rt, "a release build must bundle the runtime, but native.host.bundle is false",
			"set native.host.bundle to true (the default); a runtime found on PATH is only for development")
	}
	if in.Entry == "" {
		return "", fmt.Errorf("runtimes: BuildRelease needs ReleaseInput.Entry")
	}
	if _, err := os.Stat(in.Entry); err != nil {
		return "", fmt.Errorf("runtimes: the host bundle: %w", err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	name := in.Name
	if name == "" {
		name = "krate-host-" + string(rt)
	}
	exe := filepath.Join(outDir, target.Exe(name))
	switch rt {
	case Node:
		return exe, buildNode(target, in, exe)
	case Bun:
		return exe, buildBun(target, in, exe)
	case Deno:
		return exe, buildDeno(target, in, exe)
	case Scriptc:
		return exe, buildScriptc(target, in, exe)
	}
	return "", fmt.Errorf("runtimes: %q has no release build (it runs inside the shell)", rt)
}

// run executes a command and returns its combined output. Tests replace it.
var run = func(dir string, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	return cmd.CombinedOutput()
}

func unavailable(rt Runtime, msg, hint string) error {
	return diag.New("krate.config", 1, 1, "native.host.runtime "+string(rt)+": "+msg, hint, "").WithCode(diag.CodeRuntimeUnavailable)
}

func releaseFailed(what string, out []byte, err error) error {
	text := strings.TrimSpace(string(out))
	if len(text) > 1500 {
		text = "..." + text[len(text)-1500:]
	}
	if text == "" {
		text = err.Error()
	}
	return diag.New("krate.config", 1, 1, what+" failed:\n    "+strings.ReplaceAll(text, "\n", "\n    "),
		"run the command by hand to see the full output; see docs/native/runtimes", "").WithCode(diag.CodeRuntimeRelease)
}

var (
	// LookPath is exec.LookPath, replaceable in tests.
	lookPath = exec.LookPath
	envGet   = os.Getenv
)

// Lookup finds the executable for rt: KRATE_NODE/KRATE_BUN/KRATE_DENO, else PATH.
func Lookup(rt Runtime) (string, error) {
	env := "KRATE_" + strings.ToUpper(string(rt))
	if p := envGet(env); p != "" {
		return p, nil
	}
	p, err := lookPath(string(rt))
	if err != nil {
		return "", unavailable(rt, string(rt)+" was not found on PATH",
			"install "+string(rt)+" or set "+env+" to its executable")
	}
	return p, nil
}

var versionRe = regexp.MustCompile(`\d+(\.\d+){0,2}`)

// Version runs `<bin> --version` and returns the version number it prints.
func Version(bin string) (string, error) {
	out, err := run("", nil, bin, "--version")
	if err != nil {
		return "", err
	}
	v := versionRe.FindString(string(out))
	if v == "" {
		return "", fmt.Errorf("cannot read a version from %q", strings.TrimSpace(string(out)))
	}
	return v, nil
}

// VersionMatches reports whether have (1.2.3) satisfies want ("1", "1.2" or
// "1.2.3"; a leading v is ignored). An empty want matches everything.
func VersionMatches(want, have string) bool {
	want = strings.TrimPrefix(strings.TrimSpace(want), "v")
	if want == "" {
		return true
	}
	return have == want || strings.HasPrefix(have, want+".")
}

// checkVersion finds rt and verifies it against want.
func checkVersion(rt Runtime, want string) (string, error) {
	bin, err := Lookup(rt)
	if err != nil {
		return "", err
	}
	if want == "" {
		return bin, nil
	}
	have, err := Version(bin)
	if err != nil {
		return "", unavailable(rt, "cannot run "+bin+" --version: "+err.Error(), "check that the file is a "+string(rt)+" executable")
	}
	if !VersionMatches(want, have) {
		return "", unavailable(rt, fmt.Sprintf("%s is version %s, but native.host.version is %q", bin, have, want),
			"install the pinned version, point KRATE_"+strings.ToUpper(string(rt))+" at it, or change native.host.version")
	}
	return bin, nil
}

// SEAFuse is the sentinel Node looks for to know a binary carries a blob.
const SEAFuse = "NODE_SEA_FUSE_fce680ab2cc467b6e072b8b5df1996b2"

func buildNode(target Target, in ReleaseInput, exe string) error {
	hostNode, err := checkVersion(Node, in.Version)
	if err != nil {
		return err
	}
	// The binary that becomes the executable: this machine's node, or the
	// target's from KRATE_NODE_<OS>_<ARCH>.
	nodeBin := hostNode
	if !target.IsHost() {
		env := "KRATE_NODE_" + strings.ToUpper(target.OS) + "_" + strings.ToUpper(target.Arch)
		nodeBin = envGet(env)
		if nodeBin == "" {
			return unavailable(Node, "building the node executable for "+target.String()+" needs that platform's node binary",
				"download it from nodejs.org and set "+env+" to the path of its node executable")
		}
	}
	work := filepath.Join(filepath.Dir(exe), ".sea-"+strings.TrimSuffix(filepath.Base(exe), ".exe"))
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(work)

	// A SEA main script is a single CommonJS file, so the ES module bundle is
	// re-bundled as CJS. The bundle imports only node: built-ins.
	cjs := filepath.Join(work, "main.cjs")
	r := api.Build(api.BuildOptions{
		EntryPoints: []string{in.Entry},
		Bundle:      true,
		Format:      api.FormatCommonJS,
		Platform:    api.PlatformNode,
		Target:      api.ES2022,
		External:    []string{"node:*"},
		Outfile:     cjs,
		Write:       true,
		LogLevel:    api.LogLevelSilent,
		Sourcemap:   api.SourceMapNone,
	})
	if len(r.Errors) > 0 {
		return releaseFailed("converting the host bundle to CommonJS for the node executable", []byte(r.Errors[0].Text), fmt.Errorf("esbuild"))
	}
	blob := filepath.Join(work, "sea.blob")
	cfg, _ := json.Marshal(map[string]any{
		"main": "main.cjs", "output": "sea.blob",
		"disableExperimentalSEAWarning": true, "useCodeCache": false, "useSnapshot": false,
	})
	if err := os.WriteFile(filepath.Join(work, "sea-config.json"), cfg, 0o644); err != nil {
		return err
	}
	if out, err := run(work, nil, hostNode, "--experimental-sea-config", "sea-config.json"); err != nil {
		return releaseFailed("node --experimental-sea-config", out, err)
	}
	if err := copyFile(nodeBin, exe, 0o755); err != nil {
		return err
	}
	if target.OS == "darwin" && runtimeIsDarwin() {
		_, _ = run("", nil, "codesign", "--remove-signature", exe)
	}
	args := PostjectArgs(target, exe, blob)
	name, pre := postjectCommand()
	if out, err := run(work, nil, name, append(pre, args...)...); err != nil {
		return releaseFailed("injecting the SEA blob with postject", out, err)
	}
	if target.OS == "darwin" && runtimeIsDarwin() {
		if out, err := run("", nil, "codesign", "--sign", "-", exe); err != nil {
			return releaseFailed("codesign of the node executable", out, err)
		}
	}
	return nil
}

// PostjectArgs are the arguments of postject for a node executable.
func PostjectArgs(target Target, exe, blob string) []string {
	args := []string{exe, "NODE_SEA_BLOB", blob, "--sentinel-fuse", SEAFuse}
	if target.OS == "darwin" {
		args = append(args, "--macho-segment-name", "NODE_SEA")
	}
	return args
}

// postjectCommand prefers an installed postject, else npx (which downloads it).
func postjectCommand() (string, []string) {
	if p, err := lookPath("postject"); err == nil {
		return p, nil
	}
	return "npx", []string{"--yes", "postject"}
}

// BunCompileArgs are the arguments of `bun build --compile`.
func BunCompileArgs(target Target, entry, exe string) ([]string, error) {
	t, err := bunTarget(target)
	if err != nil {
		return nil, err
	}
	return []string{"build", "--compile", "--target=" + t, "--outfile", exe, entry}, nil
}

func buildBun(target Target, in ReleaseInput, exe string) error {
	bin, err := checkVersion(Bun, in.Version)
	if err != nil {
		return err
	}
	args, err := BunCompileArgs(target, in.Entry, exe)
	if err != nil {
		return err
	}
	if out, err := run(filepath.Dir(in.Entry), nil, bin, args...); err != nil {
		return releaseFailed("bun build --compile", out, err)
	}
	return nil
}

// DenoCompileArgs are the arguments of `deno compile`.
func DenoCompileArgs(target Target, perms []string, entry, exe string) ([]string, error) {
	t, err := denoTarget(target)
	if err != nil {
		return nil, err
	}
	args := []string{"compile", "--target", t, "--output", exe}
	for _, f := range DenoFlags(perms, false) {
		// A compiled executable cannot name the socket it will listen on, and
		// listening on a unix socket needs network access in recent Deno
		// versions, so a target that uses sockets gets plain --allow-net.
		// Windows uses loopback TCP.
		if f == "--allow-net="+LoopbackHost && target.OS != "windows" {
			f = "--allow-net"
		}
		args = append(args, f)
	}
	return append(args, entry), nil
}

func buildDeno(target Target, in ReleaseInput, exe string) error {
	bin, err := checkVersion(Deno, in.Version)
	if err != nil {
		return err
	}
	args, err := DenoCompileArgs(target, in.Permissions, in.Entry, exe)
	if err != nil {
		return err
	}
	if out, err := run(filepath.Dir(in.Entry), nil, bin, args...); err != nil {
		return releaseFailed("deno compile", out, err)
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("runtimes: %w", err)
	}
	_ = os.Remove(dst)
	return os.WriteFile(dst, b, mode)
}

func runtimeIsDarwin() bool { return HostTarget().OS == "darwin" }
