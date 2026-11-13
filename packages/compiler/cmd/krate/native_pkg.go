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
	// Arch is --arch (only "universal" exists); InstallerIdentity signs the
	// Mac App Store .pkg; MAS is set when --target mas was given.
	Arch, InstallerIdentity string
	MAS                     bool
}

// Update-related flags live in updateFlags (native_update.go).

var pkgValuedFlags = map[string]bool{
	"--format": true, "--identity": true, "--entitlements": true, "--keychain-profile": true,
	"--gpg-key": true, "--notes": true, "--update-base-url": true, "--key-out": true,
	"--pfx": true, "--thumbprint": true, "--timestamp-url": true,
	"--arch": true, "--installer-identity": true,
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
	case "--critical":
		f.Update.Critical = true
		return true, nil
	case "--uncritical":
		f.Update.Uncritical = true
		return true, nil
	case "--halt":
		f.Update.Halt = true
		return true, nil
	}
	name, inline, hasInline := strings.Cut(a, "=")
	if !pkgValuedFlags[name] && !updateValuedFlags[name] {
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
	if updateValuedFlags[name] {
		return true, f.Update.set(name, val)
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
	case "--arch":
		if val != "universal" {
			return true, fmt.Errorf("--arch takes \"universal\" (arm64 and x86_64 in one app), got %q; use --target <os>/<arch> for a single architecture", val)
		}
		p.Arch = val
	case "--installer-identity":
		p.InstallerIdentity = val
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
	universal := a == pkg.ArchUniversal && (o == "darwin" || o == "windows")
	if !ok || (o != "linux" && o != "windows" && o != "darwin") || (a != "amd64" && a != "arm64" && !universal) {
		return "", "", fmt.Errorf("unknown target %q: use <os>/<arch>, for example linux/amd64, windows/arm64, darwin/arm64 or darwin/universal (mas for the Mac App Store)", t)
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
	meta := pkg.MetadataFromConfig(root, cfg)
	pkg.ApplyAppJSON(&meta, filepath.Join(out, "app", "app.json"))
	return &pkgContext{
		root: root, cfg: cfg, meta: meta, goos: goos, goarch: goarch,
		outDir: out, pkgDir: filepath.Join(out, "packages"),
	}, nil
}

func (c *pkgContext) signConfig(flags cliFlags) pkg.SignConfig {
	return pkg.SignConfig{
		Log: os.Stderr, MacIdentity: flags.Pkg.Identity, MacEntitlements: flags.Pkg.Entitlements,
		HostRuntime: c.meta.HostRuntime, MediaUses: c.meta.MediaUses, WinPFX: flags.Pkg.PFX, WinThumbprint: flags.Pkg.Thumbprint,
		WinTimestampURL: flags.Pkg.TimestampURL, GPGKey: flags.Pkg.GPGKey,
	}
}

func (c *pkgContext) options(flags cliFlags) (pkg.Options, error) {
	key, err := pkg.LoadUpdaterKey(os.Getenv)
	if err != nil {
		return pkg.Options{}, err
	}
	rollout, err := flags.Update.rollout()
	if err != nil {
		return pkg.Options{}, err
	}
	return pkg.Options{
		Meta: c.meta, Payload: pkg.Payload{OS: c.goos, Arch: c.goarch}, OutDir: c.pkgDir, Formats: flags.Pkg.Formats,
		UpdaterKey: key, UpdateBaseURL: flags.Pkg.UpdateBaseURL, Notes: notesArg(flags.Pkg.Notes), Log: os.Stderr,
		DeltaFrom: flags.Update.DeltaFrom, DeltaCount: flags.Update.DeltaCount, Channel: flags.Update.Channel,
		Rollout: rollout, Critical: flags.Update.Critical, MinVersion: flags.Update.MinVersion,
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
		if a.Detail != "" {
			extra = " (" + a.Detail + ")"
		}
		fmt.Printf("    %-10s %s%s\n", a.Format, rel(a.Path), extra)
	}
}

// runNativePackage packages the built app for one platform.
func runNativePackage(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)
	defer startRunLog(root, cfg, "native package")()
	if err := resolveDistTargets(&flags, cfg); err != nil {
		reportPkgError(err)
		exit(1)
	}
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
	o, err := c.options(flags)
	if err != nil {
		reportPkgError(err)
		exit(1)
	}
	o.MAS = pkg.MASOptions{AppIdentity: flags.Pkg.Identity, InstallerIdentity: flags.Pkg.InstallerIdentity}
	var res *pkg.Result
	switch {
	case c.goos == "windows" && c.goarch == pkg.ArchUniversal:
		x64, e1 := pkg.Discover(c.outDir, "windows", "amd64", c.meta.ProductName)
		arm, e2 := pkg.Discover(c.outDir, "windows", "arm64", c.meta.ProductName)
		if err = errors.Join(e1, e2); err == nil {
			res, err = pkg.PackageStoreBundle(o, x64, arm)
		}
	default:
		var payload pkg.Payload
		if c.goos == "darwin" && c.goarch == pkg.ArchUniversal {
			work := filepath.Join(c.outDir, ".universal")
			defer os.RemoveAll(work)
			payload, err = pkg.DiscoverUniversal(c.outDir, c.meta.ProductName, work)
		} else {
			payload, err = pkg.Discover(c.outDir, c.goos, c.goarch, c.meta.ProductName)
		}
		if err == nil {
			o.Payload = payload
			if flags.Pkg.Sign && !flags.Pkg.MAS {
				o.Signer = pkg.NewSigner(c.signConfig(flags), c.goos)
			}
			res, err = pkg.Package(o)
		}
	}
	if err != nil {
		reportPkgError(err)
		exit(1)
	}
	if flags.Pkg.Notarize && flags.Pkg.MAS {
		reportPkgError(errors.New("--notarize does not apply to the Mac App Store: Apple reviews the app after upload"))
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
	if err := resolveDistTargets(&flags, cfg); err != nil {
		reportPkgError(err)
		exit(1)
	}
	if flags.Pkg.MAS {
		reportPkgError(errors.New("a Mac App Store build is signed while it is packaged: run krate native package --target mas --identity \"3rd Party Mac Developer Application: ...\" --installer-identity \"3rd Party Mac Developer Installer: ...\""))
		exit(1)
	}
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
	if err := resolveDistTargets(&flags, cfg); err != nil {
		reportPkgError(err)
		exit(1)
	}
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

// resolveDistTargets applies --arch universal, native.bundle.macos.universal and
// --target mas to flags.Targets and flags.Target, and picks the default
// formats of a store target. It returns an error for a combination that
// cannot work.
func resolveDistTargets(flags *cliFlags, cfg *config.Config) error {
	p := &flags.Pkg
	targets := append([]string(nil), flags.Targets...)
	n := cfg.EffectiveNative()
	cfgUniversal := n != nil && n.Bundle.MacOS.Universal
	universal := p.Arch == pkg.ArchUniversal

	for i, t := range targets {
		o, a, _ := strings.Cut(t, "/")
		if o != "mas" {
			continue
		}
		p.MAS = true
		switch {
		case a != "":
		case universal || cfgUniversal:
			a = pkg.ArchUniversal
		default:
			a = runtime.GOARCH
		}
		targets[i] = "darwin/" + a
	}
	if len(targets) == 0 {
		switch {
		case universal && runtime.GOOS == "windows":
			targets = []string{"windows/" + pkg.ArchUniversal}
		case universal || (cfgUniversal && runtime.GOOS == "darwin"):
			targets = []string{"darwin/" + pkg.ArchUniversal}
		}
	} else if universal {
		for i, t := range targets {
			o, _, _ := strings.Cut(t, "/")
			if o != "darwin" && o != "windows" {
				return fmt.Errorf("--arch universal applies to macOS (arm64 and x86_64) and Windows Store bundles (x64 and arm64), not to %s", t)
			}
			targets[i] = o + "/" + pkg.ArchUniversal
		}
	}
	seen := map[string]bool{}
	var dedup []string
	for _, t := range targets {
		if !seen[t] {
			seen[t] = true
			dedup = append(dedup, t)
		}
	}
	if len(dedup) > 0 {
		flags.Targets = dedup
		flags.Target = strings.Join(dedup, ",")
		if len(dedup) == 1 {
			flags.Target = dedup[0]
		}
	}
	if p.MAS && len(p.Formats) == 0 {
		p.Formats = []string{"mas"}
	}
	for _, t := range flags.Targets {
		if t == "windows/"+pkg.ArchUniversal && (len(p.Formats) != 1 || p.Formats[0] != "msix-store") {
			if len(p.Formats) == 0 {
				p.Formats = []string{"msix-store"}
			} else {
				return fmt.Errorf("windows/universal builds the Microsoft Store bundle; use --format msix-store")
			}
		}
	}
	return nil
}

// buildTargets expands universal targets into the architectures to compile.
func buildTargets(targets []string) []string {
	var out []string
	for _, t := range targets {
		switch t {
		case "darwin/" + pkg.ArchUniversal:
			out = append(out, "darwin/arm64", "darwin/amd64")
		case "windows/" + pkg.ArchUniversal:
			out = append(out, "windows/amd64", "windows/arm64")
		default:
			out = append(out, t)
		}
	}
	return out
}

func hasUniversal(targets []string) bool {
	for _, t := range targets {
		if strings.HasSuffix(t, "/"+pkg.ArchUniversal) {
			return true
		}
	}
	return false
}
