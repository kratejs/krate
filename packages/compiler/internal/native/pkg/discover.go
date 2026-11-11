package pkg

import (
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// BinaryTarget reports the OS and architecture an executable was built for,
// in GOOS/GOARCH names, by reading its header.
func BinaryTarget(p string) (goos, goarch string, err error) {
	if f, e := elf.Open(p); e == nil {
		defer f.Close()
		switch f.Machine {
		case elf.EM_X86_64:
			goarch = "amd64"
		case elf.EM_AARCH64:
			goarch = "arm64"
		default:
			goarch = f.Machine.String()
		}
		return "linux", goarch, nil
	}
	if f, e := pe.Open(p); e == nil {
		defer f.Close()
		switch f.Machine {
		case pe.IMAGE_FILE_MACHINE_AMD64:
			goarch = "amd64"
		case pe.IMAGE_FILE_MACHINE_ARM64:
			goarch = "arm64"
		default:
			goarch = fmt.Sprintf("0x%x", f.Machine)
		}
		return "windows", goarch, nil
	}
	if f, e := macho.Open(p); e == nil {
		defer f.Close()
		switch f.Cpu {
		case macho.CpuAmd64:
			goarch = "amd64"
		case macho.CpuArm64:
			goarch = "arm64"
		default:
			goarch = f.Cpu.String()
		}
		return "darwin", goarch, nil
	}
	if f, e := macho.OpenFat(p); e == nil {
		defer f.Close()
		return "darwin", "universal", nil
	}
	return "", "", fmt.Errorf("%s is not an ELF, PE or Mach-O executable", p)
}

func exeSuffix(goos string) string {
	if goos == "windows" {
		return ".exe"
	}
	return ""
}

// Discover finds the payload for goos/goarch under the native output
// directory (dist-native). It prefers a release executable in
// <out>/<os>-<arch>/, and falls back to the prebuilt shell layout:
// <out>/krate-shell next to <out>/app.
func Discover(outDir, goos, goarch, productName string) (Payload, error) {
	p := Payload{OS: goos, Arch: goarch, ExtraFiles: map[string]string{}}
	if bin := reportBinary(outDir, goos, goarch); bin != "" {
		// report.json (written by krate native build --release) names the
		// executable exactly, whatever the product name looks like on disk.
		dir := filepath.Dir(bin)
		p.Executable = bin
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				if e.Name() != filepath.Base(bin) {
					p.ExtraFiles[e.Name()] = filepath.Join(dir, e.Name())
				}
			}
		}
		return p, checkTarget(p)
	}
	dirs := []string{filepath.Join(outDir, goos+"-"+goarch)}
	if goos == "darwin" {
		dirs = append(dirs, filepath.Join(outDir, "macos-"+goarch))
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		exe := ""
		var execs []string
		for _, e := range entries {
			if e.Name() == productName+exeSuffix(goos) {
				exe = e.Name()
			}
			if info, err := e.Info(); err == nil && info.Mode().IsRegular() && (info.Mode()&0o111 != 0 || goos == "windows" && filepath.Ext(e.Name()) == ".exe") {
				execs = append(execs, e.Name())
			}
		}
		if exe == "" && len(execs) == 1 {
			exe = execs[0]
		}
		if exe == "" {
			continue
		}
		p.Executable = filepath.Join(dir, exe)
		for _, e := range entries {
			if e.Name() != exe {
				p.ExtraFiles[e.Name()] = filepath.Join(dir, e.Name())
			}
		}
		return p, checkTarget(p)
	}
	shell := filepath.Join(outDir, "krate-shell"+exeSuffix(goos))
	app := filepath.Join(outDir, "app")
	if _, err := os.Stat(shell); err == nil {
		if _, err := os.Stat(filepath.Join(app, "app.json")); err == nil {
			p.Executable = shell
			p.ExtraFiles["app"] = app
			return p, checkTarget(p)
		}
	}
	return p, fail(diag.CodeNativePkgInput, outDir,
		fmt.Sprintf("nothing to package for %s/%s: no %s/%s-%s/ release executable and no krate-shell + app/ pair", goos, goarch, outDir, goos, goarch),
		"build first: krate native build --release --target "+goos+"/"+goarch+" (single executable), or krate native build --shell auto (shell and app directory)")
}

// reportBinary returns the absolute path of the release executable that
// <outDir>/report.json lists for goos/goarch, or "" when there is no report,
// no such target or the file is gone.
func reportBinary(outDir, goos, goarch string) string {
	data, err := os.ReadFile(filepath.Join(outDir, "report.json"))
	if err != nil {
		return ""
	}
	var r struct {
		Targets []struct {
			Target string `json:"target"`
			Binary string `json:"binary"`
		} `json:"targets"`
	}
	if json.Unmarshal(data, &r) != nil {
		return ""
	}
	for _, t := range r.Targets {
		if t.Target != goos+"/"+goarch {
			continue
		}
		bin := filepath.Join(outDir, filepath.FromSlash(t.Binary))
		if rel, err := filepath.Rel(outDir, bin); err != nil || strings.HasPrefix(rel, "..") {
			return ""
		}
		if st, err := os.Stat(bin); err == nil && st.Mode().IsRegular() {
			return bin
		}
	}
	return ""
}

func checkTarget(p Payload) error {
	os_, arch, err := BinaryTarget(p.Executable)
	if err != nil {
		return nil // a script or an unknown format: trust the layout
	}
	if os_ != p.OS || (arch != p.Arch && arch != "universal") {
		return fail(diag.CodeNativePkgInput, p.Executable, fmt.Sprintf("the executable is built for %s/%s, not %s/%s", os_, arch, p.OS, p.Arch),
			"rebuild it with --target "+p.OS+"/"+p.Arch)
	}
	return nil
}

// PayloadFromStage rebuilds a Windows payload from a staged install tree
// (<stage>/<Name>/<Name>.exe plus its files) after signing changed it.
func PayloadFromStage(stage string, m Metadata, arch string) (Payload, error) {
	dir := filepath.Join(stage, fileSafe(m.ProductName))
	p := Payload{OS: "windows", Arch: arch, ExtraFiles: map[string]string{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return p, fail(diag.CodeNativePkgInput, dir, "no staged Windows package; run krate native package first", "")
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, n := range names {
		switch n {
		case winExeName(m):
			p.Executable = filepath.Join(dir, n)
		case "icon.ico":
		default:
			p.ExtraFiles[n] = filepath.Join(dir, n)
		}
	}
	if p.Executable == "" {
		return p, fail(diag.CodeNativePkgInput, dir, "the staged package has no "+winExeName(m), "")
	}
	return p, nil
}

// RebuildMacArchives recreates the update zip and the dmg from an already
// staged (and now signed or stapled) .app, then signs the update files.
func RebuildMacArchives(o Options, stage string) (*Result, error) {
	o.Payload.OS = "darwin"
	if o.Runner == nil {
		o.Runner = SystemRunner{}
	}
	if o.Log == nil {
		o.Log = discardWriter{}
	}
	want, err := wantedFormats(o)
	if err != nil {
		return nil, err
	}
	app := fileSafe(o.Meta.ProductName) + ".app"
	if st, err := os.Stat(filepath.Join(stage, app)); err != nil || !st.IsDir() {
		return nil, fail(diag.CodeNativePkgInput, filepath.Join(stage, app), "no staged .app; run krate native package first", "")
	}
	res := &Result{Stage: stage}
	res.add(Artifact{Path: filepath.Join(stage, app), Format: "app"})
	if err := macArchives(&o, want, stage, app, res); err != nil {
		return nil, err
	}
	if err := signUpdates(&o, res); err != nil {
		return nil, err
	}
	return res, nil
}

// ResignWindows re-packages a staged Windows install tree with signing
// enabled (the installers embed the signed executable).
func ResignWindows(o Options, stage string) (*Result, error) {
	p, err := PayloadFromStage(stage, o.Meta, o.Payload.Arch)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(stage), ".resign-")
	if err != nil {
		return nil, wrapWrite(stage, err)
	}
	defer os.RemoveAll(tmp)
	t := newTree()
	if err := t.addDisk("", filepath.Join(stage, fileSafe(o.Meta.ProductName))); err != nil {
		return nil, wrapWrite(stage, err)
	}
	if err := writeDisk(tmp, t); err != nil {
		return nil, wrapWrite(tmp, err)
	}
	p.Executable = filepath.Join(tmp, filepath.Base(p.Executable))
	for k := range p.ExtraFiles {
		p.ExtraFiles[k] = filepath.Join(tmp, k)
	}
	o.Payload = p
	return Package(o)
}

type discardWriter struct{}

func (discardWriter) Write(b []byte) (int, error) { return len(b), nil }
