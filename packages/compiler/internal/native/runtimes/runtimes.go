// Package runtimes knows how Krate Native runs TypeScript host code on
// something other than the embedded QuickJS runtime: as a child process on
// Node, Bun or Deno (the child speaks the gohost protocol, see
// packages/native/hostchild), or, behind native.experimental.scriptc, as an
// executable compiled by scriptc.
//
// It builds command lines (to run a script in development, to compile an
// executable for a release), maps native.permissions to Deno flags, and
// wraps scriptc's coverage and build commands.
package runtimes

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
)

// Runtime names a host JavaScript runtime.
type Runtime string

// The runtimes native.host.runtime and native.host.islands can name.
const (
	QuickJS Runtime = "quickjs"
	Node    Runtime = "node"
	Bun     Runtime = "bun"
	Deno    Runtime = "deno"
	Scriptc Runtime = "scriptc"
)

// Child reports whether host code runs in a child process of this runtime in
// development. QuickJS runs in the shell's process; scriptc runs on QuickJS in
// development and becomes a child only in a release build.
func (r Runtime) Child() bool { return r == Node || r == Bun || r == Deno }

// Label is what app.json and logs call the runtime.
func (r Runtime) Label() string { return string(r) }

// Target is an operating system and architecture in Go's names.
type Target struct {
	OS   string // "linux", "darwin" or "windows"
	Arch string // "amd64" or "arm64"
}

// HostTarget is the machine the compiler runs on.
func HostTarget() Target { return Target{runtime.GOOS, runtime.GOARCH} }

func (t Target) String() string { return t.OS + "/" + t.Arch }

// IsHost reports whether t is the machine the compiler runs on.
func (t Target) IsHost() bool { return t == HostTarget() }

// Exe appends ".exe" on Windows.
func (t Target) Exe(name string) string {
	if t.OS == "windows" && !strings.HasSuffix(name, ".exe") {
		return name + ".exe"
	}
	return name
}

// EntryPlaceholder stands for the absolute path of the script in a command.
const EntryPlaceholder = "{entry}"

// SockDirPlaceholder stands for the directory the shell keeps its child sockets
// in; Deno needs read and write access to it.
const SockDirPlaceholder = "{sockdir}"

// LoopbackHost is the only host a Deno child may reach without the net
// permission: the transport to the shell on Windows is loopback TCP.
const LoopbackHost = "127.0.0.1"

// Command is the argv that runs a script entry in development, with the entry
// path written as EntryPlaceholder. perms is native.permissions; only Deno
// uses it. The first element is looked up on PATH by the shell.
func Command(rt Runtime, perms []string) []string {
	switch rt {
	case Bun:
		return []string{"bun", "run", EntryPlaceholder}
	case Deno:
		cmd := []string{"deno", "run"}
		cmd = append(cmd, DenoFlags(perms, true)...)
		return append(cmd, EntryPlaceholder)
	default:
		return []string{"node", "--enable-source-maps", EntryPlaceholder}
	}
}

// DenoFlags maps native.permissions to the flags a Deno host child runs with.
// scoped is true for `deno run`, where the socket directory can be named
// (SockDirPlaceholder); a compiled executable cannot know it, so its read and
// write access to the socket is unscoped.
//
// The table (also in docs/native/runtimes.md):
//
//	always         --no-prompt --allow-env, --allow-net=127.0.0.1, read and write access to the socket directory
//	fs             --allow-read --allow-write
//	fs:read        --allow-read
//	net            --allow-net (without it only --allow-net=127.0.0.1, the Windows transport to the shell)
//	os:info        --allow-sys
//
// Every other permission names an SDK area that is served by the shell, not by
// the child, and adds no flag.
func DenoFlags(perms []string, scoped bool) []string {
	has := map[string]bool{}
	for _, p := range perms {
		has[p] = true
	}
	read, write := has["fs"] || has["fs:read"], has["fs"]
	flags := []string{"--no-prompt", "--allow-env"}
	switch {
	case read:
		flags = append(flags, "--allow-read")
	case scoped:
		flags = append(flags, "--allow-read="+SockDirPlaceholder)
	default:
		flags = append(flags, "--allow-read")
	}
	switch {
	case write:
		flags = append(flags, "--allow-write")
	case scoped:
		flags = append(flags, "--allow-write="+SockDirPlaceholder)
	default:
		flags = append(flags, "--allow-write")
	}
	if has["net"] {
		flags = append(flags, "--allow-net")
	} else {
		// The host child talks to the shell over loopback TCP on Windows
		// (Deno cannot use AF_UNIX there), so loopback is always allowed.
		flags = append(flags, "--allow-net="+LoopbackHost)
	}
	if has["os:info"] {
		flags = append(flags, "--allow-sys")
	}
	return flags
}

// DenoPermissionTable lists the permissions that map to Deno flags, for docs
// and tests.
func DenoPermissionTable() map[string][]string {
	return map[string][]string{
		"fs":      {"--allow-read", "--allow-write"},
		"fs:read": {"--allow-read"},
		"net":     {"--allow-net"},
		"os:info": {"--allow-sys"},
	}
}

// bunTarget and denoTarget translate a Target for `bun build --compile` and
// `deno compile`.
func bunTarget(t Target) (string, error) {
	osName := map[string]string{"linux": "linux", "darwin": "darwin", "windows": "windows"}[t.OS]
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[t.Arch]
	if osName == "" || arch == "" {
		return "", fmt.Errorf("bun cannot compile for %s", t)
	}
	return "bun-" + osName + "-" + arch, nil
}

func denoTarget(t Target) (string, error) {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[t.Arch]
	var rest string
	switch t.OS {
	case "linux":
		rest = "unknown-linux-gnu"
	case "darwin":
		rest = "apple-darwin"
	case "windows":
		rest = "pc-windows-msvc"
	}
	if arch == "" || rest == "" {
		return "", fmt.Errorf("deno cannot compile for %s", t)
	}
	return arch + "-" + rest, nil
}

func sortedUnique(in []string) []string {
	m := map[string]bool{}
	for _, s := range in {
		m[s] = true
	}
	out := make([]string, 0, len(m))
	for s := range m {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
