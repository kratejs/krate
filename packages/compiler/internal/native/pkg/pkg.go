// Package pkg turns a built Krate Native app into distributable packages:
// macOS .app/.zip/.dmg, Windows .zip/NSIS/MSIX, Linux .tar.gz/.deb/.rpm/
// AppImage, plus signed update artifacts.
//
// Everything that can be written in pure Go is: icons (.icns, .ico, PNG),
// Info.plist, zip and tar archives, the MSIX package and its block map, .deb,
// .rpm, the NSIS script, the Windows .syso resources. Platform tools are used
// only where no practical replacement exists (makensis, appimagetool,
// hdiutil, codesign, signtool); a missing tool is reported with a KR diagnostic
// and a hint.
package pkg

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Payload is what gets packaged, independent of how the app was built.
type Payload struct {
	// OS is "linux", "windows" or "darwin"; Arch is "amd64" or "arm64".
	OS, Arch string
	// Executable is the program to ship: a release executable with embedded
	// assets, or the prebuilt shell.
	Executable string
	// ExtraFiles maps a destination, relative to the install directory, to a
	// source file or directory ("app" -> dist-native/app). On macOS the
	// install directory is Contents/Resources.
	ExtraFiles map[string]string
}

// Metadata is the app description packaging needs.
type Metadata struct {
	AppID       string
	ProductName string
	Version     string
	Description string
	// Icon is the path of the source image, "" for a generated placeholder.
	Icon      string
	DeepLinks []string
	// FileAssociations are native.fileAssociations with icon paths made
	// absolute. See FileTypes for the defaults.
	FileAssociations []config.NativeFileAssociation
	// MacCategory is a UTI such as "public.app-category.productivity".
	MacCategory string
	// Publisher is the Windows publisher (an MSIX subject or a display name).
	Publisher string
	// Installer is "msix" or "nsis" or "".
	Installer string
	// LinuxFormats are "deb", "rpm", "appimage".
	LinuxFormats []string
	// Updater is native.updater, nil when not configured.
	Updater *config.NativeUpdater
	// HostRuntime is native.host.runtime ("quickjs", "node", ...).
	HostRuntime string
}

// MetadataFromConfig reads the packaging metadata from the native block;
// relative paths resolve against root.
func MetadataFromConfig(root string, cfg *config.Config) Metadata {
	n := cfg.EffectiveNative()
	if n == nil {
		n = &config.NativeConfig{}
	}
	m := Metadata{
		AppID: n.AppID, ProductName: n.ProductName, Version: n.Version,
		DeepLinks: n.DeepLinks, Publisher: n.Bundle.Windows.Publisher, Installer: n.Bundle.Windows.Installer,
		LinuxFormats: n.Bundle.Linux.Formats, Updater: n.Updater, HostRuntime: n.Host.Runtime,
	}
	if m.ProductName == "" {
		m.ProductName = filepath.Base(root)
	}
	if m.Version == "" {
		m.Version = "0.1.0"
	}
	if m.AppID == "" {
		m.AppID = "com.example." + strings.ReplaceAll(slug(m.ProductName), "-", "")
	}
	if n.Icons != "" {
		m.Icon = n.Icons
		if !filepath.IsAbs(m.Icon) {
			m.Icon = filepath.Join(root, m.Icon)
		}
	}
	for _, fa := range n.FileAssociations {
		fa.Ext = append([]string(nil), fa.Ext...)
		if fa.Icon != "" && !filepath.IsAbs(fa.Icon) {
			fa.Icon = filepath.Join(root, fa.Icon)
		}
		m.FileAssociations = append(m.FileAssociations, fa)
	}
	m.MacCategory = macCategory(n.Bundle.MacOS.Category)
	return m
}

// Options configure Package.
type Options struct {
	Meta    Metadata
	Payload Payload
	// OutDir is the packages directory (dist-native/packages).
	OutDir string
	// Formats overrides the formats from the metadata (--format).
	Formats []string
	// UpdaterKey signs update artifacts; nil skips update signing.
	UpdaterKey ed25519.PrivateKey
	// UpdateBaseURL is where the update artifacts will be hosted, with a
	// trailing slash; default is the directory of native.updater.url.
	UpdateBaseURL string
	// Notes goes into latest-<target>.json.
	Notes string
	// Signer, when set, signs the staged tree before archives are made and
	// the installers after (see Signer).
	Signer Signer
	// Runner runs platform tools; default the real system.
	Runner Runner
	// Log receives progress lines; default discards.
	Log io.Writer
}

// Artifact is one produced file.
type Artifact struct {
	Path   string
	Format string
	// Update marks the file the updater downloads.
	Update bool
	// Signature is the base64 updater signature of an update artifact.
	Signature string
}

// Result lists what Package produced.
type Result struct {
	// Stage is the unpacked distribution (the .app, the install tree).
	Stage     string
	Artifacts []Artifact
	// Notes are non-fatal remarks (a missing tool that only costs a format).
	Notes []string
}

func (r *Result) add(a Artifact) { r.Artifacts = append(r.Artifacts, a) }

func (r *Result) note(format string, args ...any) {
	r.Notes = append(r.Notes, fmt.Sprintf(format, args...))
}

// Error is a packaging failure that carries a diagnostic.
type Error struct{ Diag diag.Diagnostic }

func (e *Error) Error() string { return diag.FormatDiagnostics([]error{e.Diag}) }

func fail(code diag.Code, file, msg, hint string) error {
	return &Error{Diag: diag.New(file, 1, 1, msg, hint, "").WithCode(code)}
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		return "app"
	}
	return s
}

// fileSafe makes a product name usable as a file name.
func fileSafe(s string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "", "?", "", "\"", "", "<", "", ">", "", "|", "-")
	s = strings.Map(func(c rune) rune {
		if c < 0x20 || c == 0x7f {
			return -1
		}
		return c
	}, r.Replace(s))
	// ".." must not survive as a path element, and Windows drops trailing dots.
	s = strings.Trim(strings.TrimSpace(s), ". ")
	if s == "" {
		return "App"
	}
	return s
}

// oneLine collapses control characters (newlines above all) to spaces, for
// metadata formats that are line based.
func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(c rune) bool { return c < 0x20 || c == 0x7f }), " ")
}

func (m Metadata) slug() string { return slug(m.ProductName) }

// Valid formats per OS.
var formatsByOS = map[string][]string{
	"darwin":  {"app", "zip", "dmg"},
	"windows": {"zip", "nsis", "msix"},
	"linux":   {"tar.gz", "deb", "rpm", "appimage"},
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// wantedFormats returns the set of optional formats to build. The base
// archive of every OS is always built.
func wantedFormats(o Options) (map[string]bool, error) {
	valid := formatsByOS[o.Payload.OS]
	want := map[string]bool{}
	if len(o.Formats) > 0 {
		for _, f := range o.Formats {
			f = strings.ToLower(strings.TrimSpace(f))
			if f == "tgz" {
				f = "tar.gz"
			}
			if !contains(valid, f) {
				return nil, fail(diag.CodeNativePkgFormat, "--format", fmt.Sprintf("format %q is not available for %s", f, o.Payload.OS),
					"formats for "+o.Payload.OS+": "+strings.Join(valid, ", "))
			}
			want[f] = true
		}
		return want, nil
	}
	switch o.Payload.OS {
	case "darwin":
		want["dmg"] = true
	case "windows":
		if o.Meta.Installer != "" {
			want[o.Meta.Installer] = true
		}
	case "linux":
		if len(o.Meta.LinuxFormats) == 0 {
			want["deb"] = true
		}
		for _, f := range o.Meta.LinuxFormats {
			want[f] = true
		}
	}
	return want, nil
}

// validate checks the options and fills defaults.
func (o *Options) validate() error {
	p := o.Payload
	if !contains([]string{"linux", "windows", "darwin"}, p.OS) || !contains([]string{"amd64", "arm64"}, p.Arch) {
		return fail(diag.CodeNativePkgInput, "--target", fmt.Sprintf("cannot package for %s/%s", p.OS, p.Arch), "use linux, windows or darwin with amd64 or arm64")
	}
	if st, err := os.Stat(p.Executable); err != nil || !st.Mode().IsRegular() {
		return fail(diag.CodeNativePkgInput, p.Executable, "the executable to package does not exist", "build first: krate native build --release")
	}
	for dest, src := range p.ExtraFiles {
		if _, err := os.Lstat(src); err != nil {
			return fail(diag.CodeNativePkgInput, src, fmt.Sprintf("extra file %q does not exist", dest), "run krate native build first")
		}
		if cleanPath(dest) == "" {
			return fail(diag.CodeNativePkgInput, "ExtraFiles", fmt.Sprintf("bad destination %q", dest), "use a relative path such as app")
		}
	}
	m := o.Meta
	if !appIDRe.MatchString(m.AppID) {
		return fail(diag.CodeNativePkgMeta, "native.appId", fmt.Sprintf("app id %q is not a reverse-DNS identifier", m.AppID), `use dot-separated segments such as "com.acme.app"`)
	}
	if _, ok := splitVersion(m.Version); !ok {
		return fail(diag.CodeNativePkgMeta, "native.version", fmt.Sprintf("version %q is not MAJOR.MINOR.PATCH", m.Version), `use a semantic version such as "1.2.0"`)
	}
	if o.Runner == nil {
		o.Runner = SystemRunner{}
	}
	if o.Log == nil {
		o.Log = io.Discard
	}
	if o.OutDir == "" {
		return fail(diag.CodeNativePkgInput, "OutDir", "no output directory", "")
	}
	return nil
}

var appIDRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*(\.[A-Za-z][A-Za-z0-9-]*)+$`)

// Package builds the packages for one payload.
func Package(o Options) (*Result, error) {
	if err := o.validate(); err != nil {
		return nil, err
	}
	want, err := wantedFormats(o)
	if err != nil {
		return nil, err
	}
	ic, err := LoadIcon(o.Meta.Icon, o.Meta.AppID)
	if err != nil {
		return nil, fail(diag.CodeNativePkgIcon, o.Meta.Icon, err.Error(), "native.icons should be a PNG of 1024x1024 pixels")
	}
	res := &Result{}
	if ic.Generated {
		res.note("native.icons is not set; using a generated placeholder icon")
	} else if ic.Small {
		res.note("the icon is smaller than 512 pixels; large icon sizes will look soft (use 1024x1024)")
	}
	stage := filepath.Join(o.OutDir, o.Payload.OS+"-"+o.Payload.Arch)
	if err := os.RemoveAll(stage); err != nil {
		return nil, fail(diag.CodeNativePkgWrite, stage, err.Error(), "")
	}
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return nil, fail(diag.CodeNativePkgWrite, stage, err.Error(), "")
	}
	res.Stage = stage

	switch o.Payload.OS {
	case "darwin":
		err = packageMac(&o, ic, want, stage, res)
	case "windows":
		err = packageWindows(&o, ic, want, stage, res)
	case "linux":
		err = packageLinux(&o, ic, want, stage, res)
	}
	if err != nil {
		return nil, err
	}
	if err := signUpdates(&o, res); err != nil {
		return nil, err
	}
	sort.SliceStable(res.Artifacts, func(i, j int) bool { return res.Artifacts[i].Path < res.Artifacts[j].Path })
	return res, nil
}

// artifactName is "<slug>-<version>-<os>-<arch><ext>".
func artifactName(m Metadata, p Payload, ext string) string {
	return fmt.Sprintf("%s-%s-%s-%s%s", m.slug(), m.Version, p.OS, p.Arch, ext)
}

// buildTree lays the payload out in a tree. exeDest is the executable's
// path in the tree; extraBase is the directory ExtraFiles go under.
func (o *Options) buildTree(exeDest, extraBase string, follow bool) (*tree, error) {
	t := newTree()
	t.follow = follow
	t.addFile(exeDest, 0o755, o.Payload.Executable)
	for dest, src := range o.Payload.ExtraFiles {
		if err := t.addDisk(joinSlash(extraBase, dest), src); err != nil {
			return nil, fail(diag.CodeNativePkgInput, src, err.Error(), "")
		}
	}
	return t, nil
}

func joinSlash(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "/")
}

// run runs a tool, logging the redacted command line.
func (o *Options) run(dir string, env []string, name string, args ...string) ([]byte, error) {
	fmt.Fprintf(o.Log, "  $ %s\n", redactCommand(name, args))
	return o.Runner.Run(dir, env, name, args...)
}

// Runner runs platform tools. Tests replace it.
type Runner interface {
	LookPath(name string) (string, error)
	Run(dir string, env []string, name string, args ...string) ([]byte, error)
	GOOS() string
}

// SystemRunner runs real commands.
type SystemRunner struct{}

func (SystemRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }
func (SystemRunner) GOOS() string                         { return runtime.GOOS }
func (SystemRunner) Run(dir string, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

// secretFlags are options whose next argument is a secret.
var secretFlags = map[string]bool{
	"--password": true, "-p": true, "/p": true, "-pass": true, "--pass": true, "--key-password": true,
}

// redactCommand renders a command line with secret arguments replaced.
func redactCommand(name string, args []string) string {
	out := []string{name}
	hide := false
	for _, a := range args {
		switch {
		case hide:
			out = append(out, "****")
			hide = false
		case secretFlags[strings.ToLower(a)]:
			out = append(out, a)
			hide = true
		case strings.HasPrefix(strings.ToLower(a), "--password="):
			out = append(out, "--password=****")
		default:
			out = append(out, a)
		}
	}
	return strings.Join(out, " ")
}

// need returns the path of a tool or a KR7221 error.
func (o *Options) need(tool, format, hint string) (string, error) {
	p, err := o.Runner.LookPath(tool)
	if err != nil {
		return "", fail(diag.CodeNativePkgTool, "--format "+format, fmt.Sprintf("%s is needed for the %s format and was not found in PATH", tool, format), hint)
	}
	return p, nil
}

func writeFileAtomic(p string, data []byte) error {
	f, err := createFile(p)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func wrapWrite(p string, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*Error); ok {
		return err
	}
	return fail(diag.CodeNativePkgWrite, p, err.Error(), "check that the output directory is writable")
}
