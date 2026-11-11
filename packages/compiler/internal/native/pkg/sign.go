package pkg

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Environment variables the signing flows read. Secrets are never taken from
// flags and never printed.
const (
	EnvMacIdentity      = "KRATE_MACOS_IDENTITY"
	EnvWinPFX           = "KRATE_WINDOWS_PFX"
	EnvWinPFXPassword   = "KRATE_WINDOWS_PFX_PASSWORD"
	EnvWinThumbprint    = "KRATE_WINDOWS_CERT_SHA1"
	EnvWinTimestampURL  = "KRATE_WINDOWS_TIMESTAMP_URL"
	EnvMacTimestampURL  = "KRATE_MACOS_TIMESTAMP_URL"
	EnvGPGKey           = "KRATE_GPG_KEY"
	defaultWinTimestamp = "http://timestamp.digicert.com"
)

// SignConfig holds everything the signing flows need.
type SignConfig struct {
	Runner Runner
	Log    io.Writer
	// Getenv reads credentials; default os.Getenv.
	Getenv func(string) string

	// MacIdentity is a "Developer ID Application: ..." identity (or its
	// hash). "" or "-" signs ad hoc.
	MacIdentity string
	// MacEntitlements is the path of an entitlements plist; "" generates one
	// from HostRuntime.
	MacEntitlements string
	// HostRuntime is native.host.runtime and decides the generated
	// entitlements.
	HostRuntime string

	// WinPFX, WinThumbprint and WinTimestampURL select the Authenticode
	// certificate; the PFX password comes from EnvWinPFXPassword.
	WinPFX, WinThumbprint, WinTimestampURL string

	// GPGKey selects the key for detached signatures ("" is the default key).
	GPGKey string
}

func (c *SignConfig) env(k string) string {
	if c.Getenv != nil {
		return c.Getenv(k)
	}
	return os.Getenv(k)
}

func (c *SignConfig) log() io.Writer {
	if c.Log == nil {
		return io.Discard
	}
	return c.Log
}

func (c *SignConfig) runner() Runner {
	if c.Runner == nil {
		return SystemRunner{}
	}
	return c.Runner
}

func (c *SignConfig) run(name string, args ...string) ([]byte, error) {
	fmt.Fprintf(c.log(), "  $ %s\n", redactCommand(name, args))
	return c.runner().Run("", nil, name, args...)
}

func (c *SignConfig) signFailed(what string, out []byte, err error) error {
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = err.Error()
	}
	return fail(diag.CodeNativeSignFailed, what, "signing failed:\n"+indentLines(msg), "run the command above by hand to see the full error")
}

func indentLines(s string) string {
	return "    " + strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n    ")
}

// NewSigner returns the Signer for a target OS.
func NewSigner(c SignConfig, goos string) Signer {
	switch goos {
	case "darwin":
		return &macSigner{c: c}
	case "windows":
		return &winSigner{c: c}
	}
	return &linuxSigner{c: c}
}

// ---- macOS ----

// MacEntitlements returns the entitlements plist for the hardened runtime.
//
// A WKWebView app needs none: the web content (and its JIT) runs in Apple's
// WebContent process, not in the app. The embedded QuickJS interpreter and
// the Go runtime do not JIT. A bundled Node, Bun or Deno host runtime is V8
// or JavaScriptCore with a JIT and needs allow-jit and
// allow-unsigned-executable-memory. The app is not sandboxed, so the network
// client entitlement is not needed either.
func MacEntitlements(hostRuntime string) []byte {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	switch hostRuntime {
	case "node", "bun", "deno":
		b.WriteString("\t<key>com.apple.security.cs.allow-jit</key>\n\t<true/>\n")
		b.WriteString("\t<key>com.apple.security.cs.allow-unsigned-executable-memory</key>\n\t<true/>\n")
	}
	b.WriteString("</dict>\n</plist>\n")
	return []byte(b.String())
}

type macSigner struct{ c SignConfig }

func (s *macSigner) SignStage(stage string) error {
	apps, _ := filepath.Glob(filepath.Join(stage, "*.app"))
	if len(apps) == 0 {
		return fail(diag.CodeNativePkgInput, stage, "no .app bundle to sign", "run krate native package first")
	}
	for _, app := range apps {
		if err := SignMacApp(s.c, app); err != nil {
			return err
		}
	}
	return nil
}

func (s *macSigner) SignFile(path, format string) error {
	if format != "dmg" {
		return nil
	}
	if err := needCodesign(&s.c); err != nil {
		return err
	}
	id := s.c.identity()
	args := []string{"--force", "--sign", id}
	args = append(args, s.c.timestampArgs()...)
	args = append(args, path)
	if out, err := s.c.run("codesign", args...); err != nil {
		return s.c.signFailed(path, out, err)
	}
	return nil
}

func (c *SignConfig) identity() string {
	id := c.MacIdentity
	if id == "" {
		id = c.env(EnvMacIdentity)
	}
	if id == "" {
		return "-"
	}
	return id
}

func (c *SignConfig) adHoc() bool { return c.identity() == "-" }

func (c *SignConfig) timestampArgs() []string {
	if c.adHoc() {
		return []string{"--timestamp=none"}
	}
	if u := c.env(EnvMacTimestampURL); u != "" {
		return []string{"--timestamp=" + u}
	}
	return []string{"--timestamp"}
}

func needCodesign(c *SignConfig) error {
	if c.runner().GOOS() != "darwin" {
		return fail(diag.CodeNativeSignTool, "codesign", "macOS code signing only runs on macOS (codesign is part of macOS)",
			"run krate native sign on a Mac, or ship the unsigned .zip for development")
	}
	if _, err := c.runner().LookPath("codesign"); err != nil {
		return fail(diag.CodeNativeSignTool, "codesign", "codesign was not found in PATH", "install the Xcode command line tools: xcode-select --install")
	}
	return nil
}

// isMachO reports whether the file starts with a Mach-O or universal magic.
func isMachO(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	var b [4]byte
	if _, err := io.ReadFull(f, b[:]); err != nil {
		return false
	}
	switch binary.BigEndian.Uint32(b[:]) {
	case 0xfeedface, 0xfeedfacf, 0xcefaedfe, 0xcffaedfe, 0xcafebabe, 0xbebafeca:
		return true
	}
	return false
}

// nestedCode lists the code inside an app bundle that must be signed before
// the bundle: Mach-O files other than the main executable and nested
// bundles, deepest first.
func nestedCode(app string) ([]string, error) {
	var out []string
	main := ""
	if entries, err := os.ReadDir(filepath.Join(app, "Contents", "MacOS")); err == nil && len(entries) > 0 {
		main = filepath.Join(app, "Contents", "MacOS", entries[0].Name())
	}
	err := filepath.WalkDir(app, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == app {
			return nil
		}
		if d.IsDir() {
			if strings.HasSuffix(p, ".framework") || strings.HasSuffix(p, ".app") || strings.HasSuffix(p, ".xpc") {
				out = append(out, p)
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 || p == main {
			return nil
		}
		if isMachO(p) {
			out = append(out, p)
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := strings.Count(out[i], string(filepath.Separator)), strings.Count(out[j], string(filepath.Separator))
		if di != dj {
			return di > dj
		}
		return out[i] < out[j]
	})
	return out, err
}

// SignMacApp signs an app bundle with codesign: nested code first, then the
// bundle, with the hardened runtime and a secure timestamp (an ad hoc
// identity gets neither a timestamp nor a trust chain). It verifies the result.
func SignMacApp(c SignConfig, app string) error {
	if err := needCodesign(&c); err != nil {
		return err
	}
	ent := c.MacEntitlements
	if ent == "" {
		ent = filepath.Join(filepath.Dir(app), "entitlements.plist")
		if err := os.WriteFile(ent, MacEntitlements(c.HostRuntime), 0o644); err != nil {
			return wrapWrite(ent, err)
		}
	}
	id := c.identity()
	base := []string{"--force", "--options", "runtime"}
	base = append(base, c.timestampArgs()...)
	base = append(base, "--entitlements", ent, "--sign", id)
	nested, err := nestedCode(app)
	if err != nil {
		return wrapWrite(app, err)
	}
	for _, p := range append(nested, app) {
		args := append(append([]string(nil), base...), p)
		if out, err := c.run("codesign", args...); err != nil {
			return c.signFailed(p, out, err)
		}
	}
	if out, err := c.run("codesign", "--verify", "--deep", "--strict", "--verbose=2", app); err != nil {
		return c.signFailed(app, out, err)
	}
	return nil
}

// ---- notarization ----

// NotarizeConfig holds the credentials and tools of notarization.
type NotarizeConfig struct {
	Runner Runner
	Log    io.Writer
	Getenv func(string) string
	// Profile is a keychain profile made by `notarytool store-credentials`.
	Profile string
}

// Environment variables of notarization.
const (
	EnvNotaryProfile  = "KRATE_NOTARY_PROFILE"
	EnvNotaryKey      = "KRATE_NOTARY_KEY"
	EnvNotaryKeyID    = "KRATE_NOTARY_KEY_ID"
	EnvNotaryIssuer   = "KRATE_NOTARY_ISSUER"
	EnvNotaryAppleID  = "KRATE_NOTARY_APPLE_ID"
	EnvNotaryTeamID   = "KRATE_NOTARY_TEAM_ID"
	EnvNotaryPassword = "KRATE_NOTARY_PASSWORD"
)

func (c *NotarizeConfig) env(k string) string {
	if c.Getenv != nil {
		return c.Getenv(k)
	}
	return os.Getenv(k)
}

// credentialArgs picks the credential flags for notarytool.
func (c *NotarizeConfig) credentialArgs() ([]string, error) {
	profile := c.Profile
	if profile == "" {
		profile = c.env(EnvNotaryProfile)
	}
	switch {
	case profile != "":
		return []string{"--keychain-profile", profile}, nil
	case c.env(EnvNotaryKey) != "":
		key, id, issuer := c.env(EnvNotaryKey), c.env(EnvNotaryKeyID), c.env(EnvNotaryIssuer)
		if id == "" {
			return nil, fail(diag.CodeNativeNotarize, EnvNotaryKeyID, "an App Store Connect API key needs its key id", "set "+EnvNotaryKeyID+" (and "+EnvNotaryIssuer+" for a team key)")
		}
		args := []string{"--key", key, "--key-id", id}
		if issuer != "" {
			args = append(args, "--issuer", issuer)
		}
		return args, nil
	case c.env(EnvNotaryAppleID) != "":
		team, pw := c.env(EnvNotaryTeamID), c.env(EnvNotaryPassword)
		if team == "" || pw == "" {
			return nil, fail(diag.CodeNativeNotarize, EnvNotaryAppleID, "an Apple ID needs a team id and an app-specific password",
				"set "+EnvNotaryTeamID+" and "+EnvNotaryPassword+", or store a keychain profile with xcrun notarytool store-credentials")
		}
		return []string{"--apple-id", c.env(EnvNotaryAppleID), "--team-id", team, "--password", pw}, nil
	}
	return nil, fail(diag.CodeNativeNotarize, "notarize", "no notarization credentials",
		"pass --keychain-profile <name> (xcrun notarytool store-credentials), or set "+EnvNotaryKey+"/"+EnvNotaryKeyID+" for an API key")
}

// NotarizeApp submits the app to Apple, waits for the verdict and staples the
// ticket. The app must be signed with a Developer ID identity and the
// hardened runtime.
func NotarizeApp(c NotarizeConfig, app string) error {
	r := c.Runner
	if r == nil {
		r = SystemRunner{}
	}
	log := c.Log
	if log == nil {
		log = io.Discard
	}
	if r.GOOS() != "darwin" {
		return fail(diag.CodeNativeSignTool, "notarytool", "notarization only runs on macOS (xcrun notarytool)", "run krate native notarize on a Mac")
	}
	if _, err := r.LookPath("xcrun"); err != nil {
		return fail(diag.CodeNativeSignTool, "xcrun", "xcrun was not found in PATH", "install the Xcode command line tools: xcode-select --install")
	}
	creds, err := c.credentialArgs()
	if err != nil {
		return err
	}
	run := func(name string, args ...string) ([]byte, error) {
		fmt.Fprintf(log, "  $ %s\n", redactCommand(name, args))
		return r.Run("", nil, name, args...)
	}
	zipPath := filepath.Join(filepath.Dir(app), strings.TrimSuffix(filepath.Base(app), ".app")+".notarize.zip")
	defer os.Remove(zipPath)
	if out, err := run("ditto", "-c", "-k", "--keepParent", app, zipPath); err != nil {
		return fail(diag.CodeNativeSignFailed, app, "ditto failed:\n"+indentLines(string(out)), "")
	}
	args := append([]string{"notarytool", "submit", zipPath, "--wait"}, creds...)
	out, err := run("xcrun", args...)
	text := string(out)
	if err != nil || !strings.Contains(text, "status: Accepted") {
		hint := "fetch Apple's log: xcrun notarytool log <submission id> " + strings.Join(creds, " ")
		if id := submissionID(text); id != "" {
			hint = "fetch Apple's log: xcrun notarytool log " + id + " (with your credentials)"
		}
		msg := "Apple did not accept the submission:\n" + indentLines(text)
		if err != nil && text == "" {
			msg = "notarytool failed: " + err.Error()
		}
		return fail(diag.CodeNativeSignFailed, app, msg, hint)
	}
	if out, err := run("xcrun", "stapler", "staple", app); err != nil {
		return fail(diag.CodeNativeSignFailed, app, "stapler failed:\n"+indentLines(string(out)), "the app is notarized; stapling can be retried with xcrun stapler staple")
	}
	if out, err := run("xcrun", "stapler", "validate", app); err != nil {
		return fail(diag.CodeNativeSignFailed, app, "the stapled ticket does not validate:\n"+indentLines(string(out)), "")
	}
	return nil
}

// submissionID finds "id: <uuid>" in notarytool output.
func submissionID(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "id: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "id: "))
		}
	}
	return ""
}

// ---- Windows ----

type winSigner struct{ c SignConfig }

func (s *winSigner) SignStage(stage string) error {
	var files []string
	filepath.WalkDir(stage, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Type().IsRegular() {
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".exe" || ext == ".dll" {
				files = append(files, p)
			}
		}
		return nil
	})
	sort.Strings(files)
	for _, f := range files {
		if err := s.sign(f); err != nil {
			return err
		}
	}
	return nil
}

func (s *winSigner) SignFile(path, format string) error {
	if format != "nsis" && format != "msix" {
		return nil
	}
	return s.sign(path)
}

// sign signs one file with signtool, or osslsigncode where signtool is not
// available.
func (s *winSigner) sign(path string) error {
	c := &s.c
	pfx := first(c.WinPFX, c.env(EnvWinPFX))
	thumb := first(c.WinThumbprint, c.env(EnvWinThumbprint))
	ts := first(c.WinTimestampURL, c.env(EnvWinTimestampURL), defaultWinTimestamp)
	if pfx == "" && thumb == "" {
		return fail(diag.CodeNativeSignTool, "windows signing", "no signing certificate configured",
			"set "+EnvWinPFX+" (and "+EnvWinPFXPassword+") for a .pfx file, or "+EnvWinThumbprint+" for a certificate in the Windows store")
	}
	r := c.runner()
	if r.GOOS() == "windows" {
		if _, err := r.LookPath("signtool"); err == nil {
			args := []string{"sign", "/fd", "SHA256", "/tr", ts, "/td", "SHA256"}
			if pfx != "" {
				args = append(args, "/f", pfx)
				if pw := c.env(EnvWinPFXPassword); pw != "" {
					args = append(args, "/p", pw)
				}
			} else {
				args = append(args, "/sha1", thumb)
			}
			args = append(args, path)
			if out, err := c.run("signtool", args...); err != nil {
				return c.signFailed(path, out, err)
			}
			return nil
		}
	}
	if _, err := r.LookPath("osslsigncode"); err == nil {
		if pfx == "" {
			return fail(diag.CodeNativeSignTool, "osslsigncode", "osslsigncode signs with a .pfx file, not with a certificate store thumbprint", "set "+EnvWinPFX)
		}
		tmp := path + ".signed"
		args := []string{"sign", "-h", "sha256", "-ts", ts, "-pkcs12", pfx}
		if pw := c.env(EnvWinPFXPassword); pw != "" {
			args = append(args, "-pass", pw)
		}
		args = append(args, "-in", path, "-out", tmp)
		if out, err := c.run("osslsigncode", args...); err != nil {
			os.Remove(tmp)
			return c.signFailed(path, out, err)
		}
		if err := os.Rename(tmp, path); err != nil {
			return wrapWrite(path, err)
		}
		return nil
	}
	return fail(diag.CodeNativeSignTool, "signtool", "neither signtool nor osslsigncode was found in PATH",
		"on Windows install the Windows SDK (signtool); elsewhere install osslsigncode (apt install osslsigncode, brew install osslsigncode)")
}

func first(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---- Linux ----

type linuxSigner struct{ c SignConfig }

func (s *linuxSigner) SignStage(string) error { return nil }

func (s *linuxSigner) SignFile(path, format string) error {
	c := &s.c
	if _, err := c.runner().LookPath("gpg"); err != nil {
		return fail(diag.CodeNativeSignTool, "gpg", "gpg was not found in PATH", "install GnuPG, or leave out --gpg-key")
	}
	args := []string{"--batch", "--yes", "--armor", "--detach-sign"}
	if k := first(c.GPGKey, c.env(EnvGPGKey)); k != "" {
		args = append(args, "--local-user", k)
	}
	args = append(args, "--output", path+".asc", path)
	if out, err := c.run("gpg", args...); err != nil {
		return c.signFailed(path, out, err)
	}
	return nil
}
