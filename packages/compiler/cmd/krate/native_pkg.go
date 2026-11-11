package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native/pkg"
)

// nativePkgFlags are the flags of krate native package, sign, notarize and
// keygen. They are parsed by parsePkgFlag so they stay out of the long
// general flag switch.
type nativePkgFlags struct {
	Package, Sign, Notarize bool
	Formats                 []string
	Identity, Entitlements  string
	KeychainProfile         string
	GPGKey, Notes           string
	UpdateBaseURL, KeyOut   string
	PFX, Thumbprint         string
	TimestampURL            string
}

var pkgValuedFlags = map[string]bool{
	"--format": true, "--identity": true, "--entitlements": true, "--keychain-profile": true,
	"--gpg-key": true, "--notes": true, "--update-base-url": true, "--key-out": true,
	"--pfx": true, "--thumbprint": true, "--timestamp-url": true,
}

// parsePkgFlag handles one packaging flag at args[*i]. It reports whether the
// argument was one, and advances *i past a separate value.
func parsePkgFlag(f *cliFlags, args []string, i *int) (bool, error) {
	a := args[*i]
	switch a {
	case "--package":
		f.Pkg.Package = true
		return true, nil
	case "--sign":
		f.Pkg.Sign = true
		return true, nil
	case "--notarize":
		f.Pkg.Notarize = true
		return true, nil
	}
	name, inline, hasInline := strings.Cut(a, "=")
	if !pkgValuedFlags[name] {
		return false, nil
	}
	val := inline
	if !hasInline {
		if *i+1 >= len(args) {
			return true, fmt.Errorf("%s requires a value", name)
		}
		*i++
		val = args[*i]
	}
	p := &f.Pkg
	switch name {
	case "--format":
		for _, v := range strings.Split(val, ",") {
			if v = strings.TrimSpace(v); v != "" {
				p.Formats = append(p.Formats, v)
			}
		}
	case "--identity":
		p.Identity = val
	case "--entitlements":
		p.Entitlements = val
	case "--keychain-profile":
		p.KeychainProfile = val
	case "--gpg-key":
		p.GPGKey = val
	case "--notes":
		p.Notes = val
	case "--update-base-url":
		p.UpdateBaseURL = val
	case "--key-out":
		p.KeyOut = val
	case "--pfx":
		p.PFX = val
	case "--thumbprint":
		p.Thumbprint = val
	case "--timestamp-url":
		p.TimestampURL = val
	}
	return true, nil
}

// nativeHostTarget resolves --target (<os>/<arch>) to GOOS and GOARCH.
func nativeHostTarget(t string) (goos, goarch string, err error) {
	if t == "" {
		return runtime.GOOS, runtime.GOARCH, nil
	}
	o, a, ok := strings.Cut(t, "/")
	if o == "macos" {
		o = "darwin"
	}
	if !ok || (o != "linux" && o != "windows" && o != "darwin") || (a != "amd64" && a != "arm64") {
		return "", "", fmt.Errorf("unknown target %q: use <os>/<arch>, for example linux/amd64, windows/arm64 or darwin/arm64", t)
	}
	return o, a, nil
}

// reportPkgError prints a packaging error the way build errors print.
func reportPkgError(err error) {
	var pe *pkg.Error
	if errors.As(err, &pe) {
		fmt.Fprintf(os.Stderr, "%sError:%s %s\n", cRed, cReset, diag.FormatDiagnostics([]error{pe.Diag}))
		return
	}
	fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
}

type pkgContext struct {
	root         string
	cfg          *config.Config
	meta         pkg.Metadata
	goos, goarch string
	outDir       string // dist-native, absolute
	pkgDir       string // dist-native/packages
}

func newPkgContext(flags cliFlags, root string, cfg *config.Config) (*pkgContext, error) {
	goos, goarch, err := nativeHostTarget(flags.Target)
	if err != nil {
		return nil, err
	}
	out := flags.NativeOut
	if out == "" {
		out = "dist-native"
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(root, out)
	}
	return &pkgContext{
		root: root, cfg: cfg, meta: pkg.MetadataFromConfig(root, cfg), goos: goos, goarch: goarch,
		outDir: out, pkgDir: filepath.Join(out, "packages"),
	}, nil
}

func (c *pkgContext) signConfig(flags cliFlags) pkg.SignConfig {
	return pkg.SignConfig{
		Log: os.Stderr, MacIdentity: flags.Pkg.Identity, MacEntitlements: flags.Pkg.Entitlements,
		HostRuntime: c.meta.HostRuntime, WinPFX: flags.Pkg.PFX, WinThumbprint: flags.Pkg.Thumbprint,
		WinTimestampURL: flags.Pkg.TimestampURL, GPGKey: flags.Pkg.GPGKey,
	}
}

func (c *pkgContext) options(flags cliFlags) (pkg.Options, error) {
	key, err := pkg.LoadUpdaterKey(os.Getenv)
	if err != nil {
		return pkg.Options{}, err
	}
	return pkg.Options{
		Meta: c.meta, Payload: pkg.Payload{OS: c.goos, Arch: c.goarch}, OutDir: c.pkgDir, Formats: flags.Pkg.Formats,
		UpdaterKey: key, UpdateBaseURL: flags.Pkg.UpdateBaseURL, Notes: notesArg(flags.Pkg.Notes), Log: os.Stderr,
	}, nil
}

// notesArg reads release notes from text or, with a leading @, from a file.
func notesArg(s string) string {
	if strings.HasPrefix(s, "@") {
		if b, err := os.ReadFile(s[1:]); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return s
}

func printPackageResult(c *pkgContext, res *pkg.Result) {
	rel := func(p string) string {
		if r, err := filepath.Rel(c.root, p); err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
		return p
	}
	for _, n := range res.Notes {
		fmt.Printf("  %s! %s%s\n", cYellow, n, cReset)
	}
	fmt.Printf("\n  %s+%s %s %s for %s/%s\n", cGreen, cReset, c.meta.ProductName, c.meta.Version, c.goos, c.goarch)
	for _, a := range res.Artifacts {
		extra := ""
		if a.Update {
			extra = " (update file)"
		}
		fmt.Printf("    %-10s %s%s\n", a.Format, rel(a.Path), extra)
	}
}

// runNativePackage packages the built app for one platform.
func runNativePackage(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	defer startRunLog(root, cfg, "native package")()
	c, err := newPkgContext(flags, root, cfg)
	if err != nil {
		reportPkgError(err)
		exit(1)
	}
	packageNative(flags, c)
}

// packageNative runs the packaging pipeline for a prepared context.
func packageNative(flags cliFlags, c *pkgContext) {
	if flags.Pkg.Notarize && !flags.Pkg.Sign {
		reportPkgError(errors.New("--notarize needs --sign with a Developer ID identity"))
		exit(1)
	}
	fmt.Printf("%s%s  Packaging %s for %s/%s%s\n", cBold, cCyan, c.meta.ProductName, c.goos, c.goarch, cReset)
	payload, err := pkg.Discover(c.outDir, c.goos, c.goarch, c.meta.ProductName)
	if err != nil {
		reportPkgError(err)
		exit(1)
	}
	o, err := c.options(flags)
	if err != nil {
		reportPkgError(err)
		exit(1)
	}
	o.Payload = payload
	if flags.Pkg.Sign {
		o.Signer = pkg.NewSigner(c.signConfig(flags), c.goos)
	}
	res, err := pkg.Package(o)
	if err != nil {
		reportPkgError(err)
		exit(1)
	}
	if flags.Pkg.Notarize {
		if c.goos != "darwin" {
			res.Notes = append(res.Notes, "--notarize applies to macOS only; ignored")
		} else {
			app := filepath.Join(res.Stage, pkgAppName(c.meta))
			if err := pkg.NotarizeApp(pkg.NotarizeConfig{Log: os.Stderr, Profile: flags.Pkg.KeychainProfile}, app); err != nil {
				reportPkgError(err)
				exit(1)
			}
			o.Payload = pkg.Payload{OS: c.goos, Arch: c.goarch}
			if res, err = pkg.RebuildMacArchives(o, res.Stage); err != nil {
				reportPkgError(err)
				exit(1)
			}
		}
	}
	printPackageResult(c, res)
	fmt.Printf("\n%s%s  Done!%s\n", cBold, cGreen, cReset)
}

func pkgAppName(m pkg.Metadata) string { return pkg.MacAppName(m) }

// runNativeSign signs the packaged output of one platform and rebuilds the
// archives from the signed files.
func runNativeSign(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	c, err := newPkgContext(flags, root, cfg)
	if err != nil {
		reportPkgError(err)
		exit(1)
	}
	stage := filepath.Join(c.pkgDir, c.goos+"-"+c.goarch)
	if _, err := os.Stat(stage); err != nil {
		reportPkgError(fmt.Errorf("nothing to sign: %s does not exist; run krate native package --target %s/%s first", stage, c.goos, c.goarch))
		exit(1)
	}
	o, err := c.options(flags)
	if err != nil {
		reportPkgError(err)
		exit(1)
	}
	sc := c.signConfig(flags)
	var res *pkg.Result
	switch c.goos {
	case "darwin":
		if err = pkg.NewSigner(sc, "darwin").SignStage(stage); err == nil {
			res, err = pkg.RebuildMacArchives(o, stage)
		}
	case "windows":
		o.Signer = pkg.NewSigner(sc, "windows")
		res, err = pkg.ResignWindows(o, stage)
	default:
		res, err = pkg.SignLinuxArtifacts(pkg.NewSigner(sc, "linux"), c.pkgDir, c.meta)
	}
	if err != nil {
		reportPkgError(err)
		exit(1)
	}
	printPackageResult(c, res)
	fmt.Printf("\n%s%s  Signed.%s\n", cBold, cGreen, cReset)
}

// runNativeNotarize notarizes and staples the signed macOS app, then rebuilds
// the archives.
func runNativeNotarize(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	if flags.Target == "" {
		flags.Target = "darwin/" + runtime.GOARCH
	}
	c, err := newPkgContext(flags, root, cfg)
	if err != nil {
		reportPkgError(err)
		exit(1)
	}
	if c.goos != "darwin" {
		reportPkgError(fmt.Errorf("notarization is for macOS apps; --target %s/%s is not", c.goos, c.goarch))
		exit(1)
	}
	stage := filepath.Join(c.pkgDir, c.goos+"-"+c.goarch)
	app := filepath.Join(stage, pkgAppName(c.meta))
	if _, err := os.Stat(app); err != nil {
		reportPkgError(fmt.Errorf("nothing to notarize: %s does not exist; run krate native package --sign first", app))
		exit(1)
	}
	if err := pkg.NotarizeApp(pkg.NotarizeConfig{Log: os.Stderr, Profile: flags.Pkg.KeychainProfile}, app); err != nil {
		reportPkgError(err)
		exit(1)
	}
	o, err := c.options(flags)
	if err != nil {
		reportPkgError(err)
		exit(1)
	}
	res, err := pkg.RebuildMacArchives(o, stage)
	if err != nil {
		reportPkgError(err)
		exit(1)
	}
	printPackageResult(c, res)
	fmt.Printf("\n%s%s  Notarized and stapled.%s\n", cBold, cGreen, cReset)
}

// runNativeKeygen creates the updater key pair.
func runNativeKeygen(flags cliFlags) {
	out := flags.Pkg.KeyOut
	if out == "" {
		out = "krate-updater.key"
	}
	pub, err := pkg.GenerateUpdaterKey(out, flags.Force)
	if err != nil {
		reportPkgError(err)
		exit(1)
	}
	fmt.Printf("%s%s  Wrote the updater private key to %s%s\n", cBold, cGreen, out, cReset)
	fmt.Printf("  Keep it secret and out of git; give it to CI as %s.\n\n", pkg.EnvUpdaterKey)
	fmt.Printf("  Public key (put it in the config):\n\n    %s\n\n", pub)
	fmt.Printf("  native: { updater: { url: \"https://example.com/updates/latest-{target}.json\", publicKey: %q } }\n", pub)
}
