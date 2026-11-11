package pkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func machoFile(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	// A 64-bit little-endian Mach-O magic is enough for isMachO.
	if err := os.WriteFile(p, []byte{0xcf, 0xfa, 0xed, 0xfe, 0, 0, 0, 0}, 0o755); err != nil {
		t.Fatal(err)
	}
}

func stagedApp(t *testing.T) (stage, app string) {
	t.Helper()
	stage = t.TempDir()
	app = filepath.Join(stage, "Acme Notes.app")
	machoFile(t, filepath.Join(app, "Contents/MacOS/Acme Notes"))
	machoFile(t, filepath.Join(app, "Contents/Resources/app/gohost/krate-gohost"))
	machoFile(t, filepath.Join(app, "Contents/Frameworks/libx.dylib"))
	if err := os.WriteFile(filepath.Join(app, "Contents/Resources/app/app.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	return
}

func TestSignMacAppDeepSignsInnerFirst(t *testing.T) {
	stage, app := stagedApp(t)
	fr := &fakeRunner{goos: "darwin", tools: map[string]bool{"codesign": true}}
	err := SignMacApp(SignConfig{Runner: fr, MacIdentity: "Developer ID Application: Acme (ABCDE12345)", HostRuntime: "node"}, app)
	if err != nil {
		t.Fatal(err)
	}
	if len(fr.calls) != 4 { // two nested, the bundle, verify
		t.Fatalf("calls:\n%s", strings.Join(fr.calls, "\n"))
	}
	for _, c := range fr.calls[:3] {
		for _, want := range []string{"codesign --force --options runtime --timestamp --entitlements ", "--sign Developer ID Application: Acme (ABCDE12345) "} {
			if !strings.Contains(c, want) {
				t.Errorf("%q lacks %q", c, want)
			}
		}
	}
	if !strings.HasSuffix(fr.calls[2], "Acme Notes.app") {
		t.Errorf("the bundle must be signed last: %s", fr.calls[2])
	}
	if strings.HasSuffix(fr.calls[0], "Acme Notes") || strings.HasSuffix(fr.calls[1], "Acme Notes") {
		t.Errorf("the main executable is signed with the bundle, not before")
	}
	if !strings.HasPrefix(fr.calls[3], "codesign --verify --deep --strict") {
		t.Errorf("no verification: %s", fr.calls[3])
	}
	ent, err := os.ReadFile(filepath.Join(stage, "entitlements.plist"))
	if err != nil || !strings.Contains(string(ent), "allow-jit") {
		t.Fatalf("node host runtime needs the JIT entitlement: %s %v", ent, err)
	}
}

func TestSignMacAdHocAndEntitlements(t *testing.T) {
	_, app := stagedApp(t)
	fr := &fakeRunner{goos: "darwin", tools: map[string]bool{"codesign": true}}
	if err := SignMacApp(SignConfig{Runner: fr, Getenv: env(nil)}, app); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fr.calls[0], "--timestamp=none") || !strings.Contains(fr.calls[0], "--sign - ") {
		t.Fatalf("ad hoc flags: %s", fr.calls[0])
	}
	if got := string(MacEntitlements("quickjs")); strings.Contains(got, "allow-jit") || !strings.Contains(got, "<dict>") {
		t.Fatalf("quickjs needs no JIT entitlement:\n%s", got)
	}
	// Identity from the environment.
	fr = &fakeRunner{goos: "darwin", tools: map[string]bool{"codesign": true}}
	if err := SignMacApp(SignConfig{Runner: fr, Getenv: env(map[string]string{EnvMacIdentity: "Dev ID X"})}, app); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fr.calls[0], "--sign Dev ID X ") || !strings.Contains(fr.calls[0], "--timestamp ") {
		t.Fatalf("%s", fr.calls[0])
	}
}

func TestSignMacOnlyOnMac(t *testing.T) {
	_, app := stagedApp(t)
	err := SignMacApp(SignConfig{Runner: &fakeRunner{goos: "linux", tools: map[string]bool{"codesign": true}}}, app)
	if pe, ok := err.(*Error); !ok || pe.Diag.Code != "KR7224" || !strings.Contains(pe.Diag.Message, "only runs on macOS") {
		t.Fatalf("%v", err)
	}
	err = SignMacApp(SignConfig{Runner: &fakeRunner{goos: "darwin"}}, app)
	if pe, ok := err.(*Error); !ok || pe.Diag.Code != "KR7224" {
		t.Fatalf("%v", err)
	}
	fr := &fakeRunner{goos: "darwin", tools: map[string]bool{"codesign": true}, fail: map[string]bool{"codesign": true}}
	err = SignMacApp(SignConfig{Runner: fr}, app)
	if pe, ok := err.(*Error); !ok || pe.Diag.Code != "KR7225" || !strings.Contains(pe.Diag.Message, "boom") {
		t.Fatalf("%v", err)
	}
}

func TestNotarize(t *testing.T) {
	_, app := stagedApp(t)
	// No credentials.
	fr := &fakeRunner{goos: "darwin", tools: map[string]bool{"xcrun": true}}
	err := NotarizeApp(NotarizeConfig{Runner: fr, Getenv: env(nil)}, app)
	if pe, ok := err.(*Error); !ok || pe.Diag.Code != "KR7226" {
		t.Fatalf("%v", err)
	}
	// Profile: submit --wait, then staple and validate. The fake does not
	// print "status: Accepted", so make one that does.
	acc := &acceptRunner{fakeRunner: fakeRunner{goos: "darwin", tools: map[string]bool{"xcrun": true}}}
	if err := NotarizeApp(NotarizeConfig{Runner: acc, Profile: "krate-profile", Getenv: env(nil)}, app); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(acc.calls, "\n")
	for _, want := range []string{"xcrun notarytool submit ", "--wait --keychain-profile krate-profile", "xcrun stapler staple ", "xcrun stapler validate ", "ditto -c -k --keepParent "} {
		if !strings.Contains(joined, want) {
			t.Errorf("calls lack %q:\n%s", want, joined)
		}
	}
	// API key credentials.
	acc = &acceptRunner{fakeRunner: fakeRunner{goos: "darwin", tools: map[string]bool{"xcrun": true}}}
	if err := NotarizeApp(NotarizeConfig{Runner: acc, Getenv: env(map[string]string{EnvNotaryKey: "/k/AuthKey.p8", EnvNotaryKeyID: "K1", EnvNotaryIssuer: "I1"})}, app); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(acc.calls, "\n"), "--key /k/AuthKey.p8 --key-id K1 --issuer I1") {
		t.Fatalf("%v", acc.calls)
	}
	// A rejected submission reports Apple's output and the log hint.
	rej := &acceptRunner{fakeRunner: fakeRunner{goos: "darwin", tools: map[string]bool{"xcrun": true}}, out: "  id: 1234-abcd\n  status: Invalid\n"}
	err = NotarizeApp(NotarizeConfig{Runner: rej, Profile: "p", Getenv: env(nil)}, app)
	pe, ok := err.(*Error)
	if !ok || pe.Diag.Code != "KR7225" || !strings.Contains(pe.Diag.Hint, "1234-abcd") {
		t.Fatalf("%v", err)
	}
	// Elsewhere.
	err = NotarizeApp(NotarizeConfig{Runner: &fakeRunner{goos: "linux"}, Profile: "p", Getenv: env(nil)}, app)
	if pe, ok := err.(*Error); !ok || pe.Diag.Code != "KR7224" {
		t.Fatalf("%v", err)
	}
	// Password never reaches a log line.
	var log strings.Builder
	acc = &acceptRunner{fakeRunner: fakeRunner{goos: "darwin", tools: map[string]bool{"xcrun": true}}}
	if err := NotarizeApp(NotarizeConfig{Runner: acc, Log: &log, Getenv: env(map[string]string{EnvNotaryAppleID: "a@b.c", EnvNotaryTeamID: "T", EnvNotaryPassword: "s3cret-pw"})}, app); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(log.String(), "s3cret-pw") || !strings.Contains(log.String(), "--password ****") {
		t.Fatalf("log leaks the password:\n%s", log.String())
	}
}

type acceptRunner struct {
	fakeRunner
	out string
}

func (a *acceptRunner) Run(dir string, env []string, name string, args ...string) ([]byte, error) {
	if _, err := a.fakeRunner.Run(dir, env, name, args...); err != nil {
		return nil, err
	}
	if name == "xcrun" && len(args) > 1 && args[0] == "notarytool" && args[1] == "submit" {
		if a.out != "" {
			return []byte(a.out), nil
		}
		return []byte("  id: abc\n  status: Accepted\n"), nil
	}
	return nil, nil
}

func TestSignWindows(t *testing.T) {
	stage := t.TempDir()
	if err := os.MkdirAll(filepath.Join(stage, "Acme Notes", "app", "gohost"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "Acme Notes", "Acme Notes.exe"), []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "Acme Notes", "app", "gohost", "krate-gohost.exe"), []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "Acme Notes", "icon.ico"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// signtool on Windows with a pfx.
	fr := &fakeRunner{goos: "windows", tools: map[string]bool{"signtool": true}}
	s := NewSigner(SignConfig{Runner: fr, Getenv: env(map[string]string{EnvWinPFX: `C:\c.pfx`, EnvWinPFXPassword: "pw"})}, "windows")
	if err := s.SignStage(stage); err != nil {
		t.Fatal(err)
	}
	if len(fr.calls) != 2 {
		t.Fatalf("%v", fr.calls)
	}
	c := fr.calls[0]
	for _, want := range []string{"signtool sign /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 /f C:\\c.pfx /p pw "} {
		if !strings.Contains(c, want) {
			t.Errorf("%q lacks %q", c, want)
		}
	}
	// Thumbprint instead of a file.
	fr = &fakeRunner{goos: "windows", tools: map[string]bool{"signtool": true}}
	s = NewSigner(SignConfig{Runner: fr, Getenv: env(map[string]string{EnvWinThumbprint: "ABCD"})}, "windows")
	if err := s.SignFile("setup.exe", "nsis"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fr.calls[0], "/sha1 ABCD setup.exe") || strings.Contains(fr.calls[0], "/f ") {
		t.Fatalf("%s", fr.calls[0])
	}
	// A format that is not signed is skipped.
	if err := s.SignFile("x.zip", "zip"); err != nil || len(fr.calls) != 1 {
		t.Fatalf("zip was signed")
	}
	// osslsigncode on Linux.
	dir := t.TempDir()
	target := filepath.Join(dir, "setup.exe")
	if err := os.WriteFile(target, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	osl := &osslRunner{fakeRunner: fakeRunner{goos: "linux", tools: map[string]bool{"osslsigncode": true}}}
	s = NewSigner(SignConfig{Runner: osl, Getenv: env(map[string]string{EnvWinPFX: "/c.pfx", EnvWinPFXPassword: "pw", EnvWinTimestampURL: "http://ts.example"})}, "windows")
	if err := s.SignFile(target, "nsis"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(osl.calls[0], "osslsigncode sign -h sha256 -ts http://ts.example -pkcs12 /c.pfx -pass pw -in "+target+" -out "+target+".signed") {
		t.Fatalf("%s", osl.calls[0])
	}
	if b, _ := os.ReadFile(target); string(b) != "MZ-signed" {
		t.Fatalf("signed file not moved into place: %q", b)
	}
	// Nothing available / nothing configured.
	s = NewSigner(SignConfig{Runner: &fakeRunner{goos: "linux"}, Getenv: env(map[string]string{EnvWinPFX: "/c.pfx"})}, "windows")
	if err := s.SignFile("a.exe", "nsis"); err == nil || !strings.Contains(err.Error(), "KR7224") {
		t.Fatalf("%v", err)
	}
	s = NewSigner(SignConfig{Runner: &fakeRunner{goos: "windows", tools: map[string]bool{"signtool": true}}, Getenv: env(nil)}, "windows")
	if err := s.SignFile("a.exe", "nsis"); err == nil || !strings.Contains(err.Error(), "no signing certificate") {
		t.Fatalf("%v", err)
	}
}

type osslRunner struct{ fakeRunner }

func (o *osslRunner) Run(dir string, env []string, name string, args ...string) ([]byte, error) {
	if _, err := o.fakeRunner.Run(dir, env, name, args...); err != nil {
		return nil, err
	}
	for i, a := range args {
		if a == "-out" {
			if err := os.WriteFile(args[i+1], []byte("MZ-signed"), 0o755); err != nil {
				return nil, err
			}
		}
	}
	return nil, nil
}

func TestSignLinuxGPG(t *testing.T) {
	fr := &fakeRunner{goos: "linux", tools: map[string]bool{"gpg": true}}
	s := NewSigner(SignConfig{Runner: fr, GPGKey: "ABCD1234"}, "linux")
	if err := s.SignFile("/o/app.deb", "deb"); err != nil {
		t.Fatal(err)
	}
	if fr.calls[0] != "gpg --batch --yes --armor --detach-sign --local-user ABCD1234 --output /o/app.deb.asc /o/app.deb" {
		t.Fatalf("%s", fr.calls[0])
	}
	s = NewSigner(SignConfig{Runner: &fakeRunner{goos: "linux"}}, "linux")
	if err := s.SignFile("/o/app.deb", "deb"); err == nil || !strings.Contains(err.Error(), "KR7224") {
		t.Fatalf("%v", err)
	}
}

// Signing during packaging happens before the archives are built, so the
// archives carry the signed tree.
func TestPackageSignsBeforeArchiving(t *testing.T) {
	m := testMeta(t)
	m.Installer = "nsis"
	order := &orderSigner{}
	fr := &fakeRunner{goos: "linux", tools: map[string]bool{"makensis": true}}
	res := run(t, Options{Meta: m, Payload: testPayload(t, "windows", "amd64"), Signer: order, Runner: fr})
	if strings.Join(order.log, ",") != "stage,file:nsis" {
		t.Fatalf("%v", order.log)
	}
	es := zipEntries(t, find(t, res, "zip").Path)
	rc, _ := es["Acme Notes/Acme Notes.exe"].Open()
	b := make([]byte, 64)
	n, _ := rc.Read(b)
	rc.Close()
	if !strings.Contains(string(b[:n]), "SIGNED") {
		t.Fatalf("zip holds the unsigned exe: %q", b[:n])
	}
}

type orderSigner struct{ log []string }

func (o *orderSigner) SignStage(stage string) error {
	o.log = append(o.log, "stage")
	exe := filepath.Join(stage, "Acme Notes", "Acme Notes.exe")
	b, _ := os.ReadFile(exe)
	return os.WriteFile(exe, append(b, []byte("SIGNED")...), 0o755)
}

func (o *orderSigner) SignFile(p, format string) error {
	o.log = append(o.log, "file:"+format)
	return nil
}

func TestDiscoverAndResign(t *testing.T) {
	out := t.TempDir()
	// Nothing built.
	if _, err := Discover(out, "linux", "amd64", "Acme Notes"); err == nil || !strings.Contains(err.Error(), "KR7220") {
		t.Fatalf("%v", err)
	}
	// Shell layout.
	if err := os.WriteFile(filepath.Join(out, "krate-shell"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(out, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "app", "app.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Discover(out, "linux", "amd64", "Acme Notes")
	if err != nil || p.ExtraFiles["app"] != filepath.Join(out, "app") || filepath.Base(p.Executable) != "krate-shell" {
		t.Fatalf("%+v %v", p, err)
	}
	// A release executable wins.
	rel := filepath.Join(out, "linux-amd64")
	if err := os.MkdirAll(rel, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rel, "Acme Notes"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err = Discover(out, "linux", "amd64", "Acme Notes")
	if err != nil || filepath.Base(p.Executable) != "Acme Notes" || len(p.ExtraFiles) != 0 {
		t.Fatalf("%+v %v", p, err)
	}
	// Windows resign rebuilds the archives from the staged tree.
	m := testMeta(t)
	po := t.TempDir()
	res, err := Package(Options{Meta: m, Payload: testPayload(t, "windows", "amd64"), OutDir: po, Runner: &fakeRunner{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(res.Stage, "Acme Notes", "Acme Notes.exe"), []byte("re-signed"), 0o755); err != nil {
		t.Fatal(err)
	}
	order := &orderSigner{}
	res2, err := ResignWindows(Options{Meta: m, Payload: Payload{OS: "windows", Arch: "amd64"}, OutDir: po, Runner: &fakeRunner{}, Signer: order}, res.Stage)
	if err != nil {
		t.Fatal(err)
	}
	es := zipEntries(t, find(t, res2, "zip").Path)
	rc, _ := es["Acme Notes/Acme Notes.exe"].Open()
	b := make([]byte, 64)
	n, _ := rc.Read(b)
	rc.Close()
	if !strings.HasPrefix(string(b[:n]), "re-signed") {
		t.Fatalf("resign lost the staged exe: %q", b[:n])
	}
}

func TestBinaryTarget(t *testing.T) {
	// The test binary itself is the host's executable format.
	exe, _ := os.Executable()
	goos, _, err := BinaryTarget(exe)
	if err != nil || goos == "" {
		t.Fatalf("%v", err)
	}
	if _, _, err := BinaryTarget(os.Args[0] + ".nope"); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestRebuildMacArchives(t *testing.T) {
	m := testMeta(t)
	po := t.TempDir()
	res, err := Package(Options{Meta: m, Payload: testPayload(t, "darwin", "arm64"), OutDir: po, Runner: &fakeRunner{}})
	if err != nil {
		t.Fatal(err)
	}
	app := find(t, res, "app").Path
	if err := os.WriteFile(filepath.Join(app, "Contents", "Stapled"), []byte("ticket"), 0o644); err != nil {
		t.Fatal(err)
	}
	res2, err := RebuildMacArchives(Options{Meta: m, Payload: Payload{Arch: "arm64"}, OutDir: po, Runner: &fakeRunner{}}, res.Stage)
	if err != nil {
		t.Fatal(err)
	}
	if es := zipEntries(t, find(t, res2, "zip").Path); es["Acme Notes.app/Contents/Stapled"] == nil {
		t.Fatal("rebuilt zip lacks the stapled ticket")
	}
}
