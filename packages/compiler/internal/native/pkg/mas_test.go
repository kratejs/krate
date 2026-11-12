package pkg

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func masMeta(t *testing.T) Metadata {
	m := testMeta(t)
	m.MAS = config.NativeMAS{TeamID: "ABCDE12345"}
	m.Permissions = []string{"net", "fs", "notify"}
	return m
}

func fakeProfile(t *testing.T, content string) string {
	p := filepath.Join(t.TempDir(), "app.provisionprofile")
	if err := os.WriteFile(p, []byte("\x30\x82bin<?xml version=\"1.0\"?><plist><dict><key>com.apple.application-identifier</key><string>"+content+"</string></dict></plist>"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func entKeys(es []plistEntry) map[string]string {
	m := map[string]string{}
	for _, e := range es {
		m[e.Key] = e.Str
	}
	return m
}

func TestMASEntitlementsFollowPermissions(t *testing.T) {
	m := masMeta(t)
	m.MediaUses = []string{"camera", "microphone", "location"}
	k := entKeys(masEntitlements(m))
	for _, want := range []string{
		"com.apple.security.app-sandbox", "com.apple.security.network.client", "com.apple.security.files.user-selected.read-write",
		"com.apple.security.device.camera", "com.apple.security.device.audio-input", "com.apple.security.personal-information.location",
		"com.apple.application-identifier", "com.apple.developer.team-identifier",
	} {
		if _, ok := k[want]; !ok {
			t.Errorf("missing %s in %v", want, k)
		}
	}
	if k["com.apple.application-identifier"] != "ABCDE12345.com.acme.notes" || k["com.apple.developer.team-identifier"] != "ABCDE12345" {
		t.Errorf("identifiers = %v", k)
	}
	if _, ok := k["com.apple.security.cs.allow-jit"]; ok {
		t.Error("no JIT without a JavaScript host")
	}
	m.HostRuntimes = []string{"node"}
	m.MAS.Network = []string{"client", "server"}
	m.MAS.Files = "read-only"
	k = entKeys(masEntitlements(m))
	if _, ok := k["com.apple.security.cs.allow-jit"]; !ok {
		t.Error("a Node host needs allow-jit")
	}
	if _, ok := k["com.apple.security.network.server"]; !ok {
		t.Error("network.server missing")
	}
	if _, ok := k["com.apple.security.files.user-selected.read-only"]; !ok {
		t.Error("read-only missing")
	}
	if _, ok := k["com.apple.security.files.user-selected.read-write"]; ok {
		t.Error("read-write must not be set with read-only")
	}
	m.MAS.Files = "none"
	for key := range entKeys(masEntitlements(m)) {
		if strings.Contains(key, "user-selected") {
			t.Error("files none still grants " + key)
		}
	}
}

func TestEntitlementsPlistIsWellFormed(t *testing.T) {
	b := entitlementsPlist(masEntitlements(masMeta(t)))
	var v struct{}
	if err := xml.Unmarshal(b, &v); err != nil {
		t.Fatalf("plist is not XML: %v\n%s", err, b)
	}
	s := string(b)
	if !strings.Contains(s, "<key>com.apple.security.app-sandbox</key>\n\t<true/>") ||
		!strings.Contains(s, "<string>ABCDE12345.com.acme.notes</string>") {
		t.Errorf("plist:\n%s", s)
	}
	child := string(entitlementsPlist(masChildEntitlements(masMeta(t), false)))
	if !strings.Contains(child, "com.apple.security.inherit") || strings.Contains(child, "network") {
		t.Errorf("child plist:\n%s", child)
	}
}

func TestStripUpdaterJSON(t *testing.T) {
	in := []byte(`{"appId":"a.b.c","updater":{"url":"https://x/y","publicKey":"k"},"permissions":["kv","updater","net"]}`)
	out, changed, err := stripUpdaterJSON(in)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	s := string(out)
	if strings.Contains(s, "updater") || !strings.Contains(s, `"net"`) || !strings.Contains(s, `"appId"`) {
		t.Errorf("out = %s", s)
	}
	if _, changed, _ := stripUpdaterJSON([]byte(`{"appId":"x"}`)); changed {
		t.Error("nothing to strip")
	}
}

func masOptions(t *testing.T, m Metadata, r Runner) Options {
	pl := testPayload(t, "darwin", "arm64")
	return Options{Meta: m, Payload: pl, Formats: []string{"mas"}, Runner: r, OutDir: filepath.Join(t.TempDir(), "packages")}
}

func TestPackageMASAdHocSignsWithSandbox(t *testing.T) {
	r := &fakeRunner{goos: "darwin", tools: map[string]bool{"codesign": true, "productbuild": true}}
	m := masMeta(t)
	m.Updater = &config.NativeUpdater{URL: "https://a.b/u.json", PublicKey: "k"}
	m.MAS.TeamID = ""
	o := masOptions(t, m, r)
	// Put a host child in the payload so nested signing is exercised.
	hostDir := t.TempDir()
	os.WriteFile(filepath.Join(hostDir, "krate-host-node"), thinMachO(0x0100000c, 0, "x"), 0o755)
	o.Payload.ExtraFiles["host"] = hostDir
	res, err := Package(o)
	if err != nil {
		t.Fatal(err)
	}
	var codesigns []string
	for _, c := range r.calls {
		if strings.HasPrefix(c, "codesign") {
			codesigns = append(codesigns, c)
		}
	}
	if len(codesigns) < 3 {
		t.Fatalf("expected nested, app and verify codesign calls, got %v", r.calls)
	}
	if !strings.Contains(codesigns[0], "mas-child-jit.entitlements") || !strings.Contains(codesigns[0], "krate-host-node") {
		t.Errorf("nested sign: %s", codesigns[0])
	}
	if !strings.Contains(codesigns[1], "mas-app.entitlements") || !strings.Contains(codesigns[1], "--sign - ") || strings.Contains(codesigns[1], "--options") {
		t.Errorf("app sign: %s", codesigns[1])
	}
	app := find(t, res, "app").Path
	ent, err := os.ReadFile(filepath.Join(filepath.Dir(app), "mas-app.entitlements"))
	if err != nil || !strings.Contains(string(ent), "app-sandbox") || strings.Contains(string(ent), "application-identifier") {
		t.Errorf("ad hoc entitlements must hold the sandbox and no restricted ids: %s %v", ent, err)
	}
	pkgArt := find(t, res, "mas")
	if !strings.HasSuffix(pkgArt.Path, "acme-notes-1.2.0-darwin-arm64-mas.pkg") {
		t.Errorf("pkg = %s", pkgArt.Path)
	}
	last := r.calls[len(r.calls)-1]
	if !strings.HasPrefix(last, "productbuild --component ") || !strings.Contains(last, " /Applications ") || strings.Contains(last, "--sign") {
		t.Errorf("productbuild call: %s", last)
	}
	for _, a := range res.Artifacts {
		if a.Update || a.Format == "zip" || a.Format == "dmg" {
			t.Errorf("a store build has no update file: %+v", a)
		}
	}
	joined := strings.Join(res.Notes, "\n")
	if !strings.Contains(joined, "native.updater is not used") || !strings.Contains(joined, "no provisioning profile") || !strings.Contains(joined, "unsigned") {
		t.Errorf("notes:\n%s", joined)
	}
	b, _ := os.ReadFile(filepath.Join(app, "Contents", "Resources", "krate-dist.json"))
	if !strings.Contains(string(b), `"store": "mas"`) {
		t.Errorf("marker = %s", b)
	}
	plist, _ := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
	if !strings.Contains(string(plist), "CFBundleSupportedPlatforms") {
		t.Error("Info.plist lacks CFBundleSupportedPlatforms")
	}
}

func TestPackageMASRealIdentity(t *testing.T) {
	r := &fakeRunner{goos: "darwin", tools: map[string]bool{"codesign": true, "productbuild": true}}
	m := masMeta(t)
	m.MAS.ProvisioningProfile = fakeProfile(t, "ABCDE12345.com.acme.notes")
	o := masOptions(t, m, r)
	o.MAS = MASOptions{AppIdentity: "3rd Party Mac Developer Application: Acme (ABCDE12345)", InstallerIdentity: "3rd Party Mac Developer Installer: Acme (ABCDE12345)", Getenv: func(string) string { return "" }}
	res, err := Package(o)
	if err != nil {
		t.Fatal(err)
	}
	app := find(t, res, "app").Path
	if _, err := os.Stat(filepath.Join(app, "Contents", "embedded.provisionprofile")); err != nil {
		t.Errorf("profile not embedded: %v", err)
	}
	ent, _ := os.ReadFile(filepath.Join(filepath.Dir(app), "mas-app.entitlements"))
	if !strings.Contains(string(ent), "<string>ABCDE12345.com.acme.notes</string>") {
		t.Errorf("entitlements lack the application identifier:\n%s", ent)
	}
	signed := false
	for _, c := range r.calls {
		signed = signed || strings.HasPrefix(c, "productbuild") && strings.Contains(c, "--sign 3rd Party Mac Developer Installer: Acme (ABCDE12345)")
		if strings.HasPrefix(c, "codesign --force") && strings.Contains(c, "--timestamp=none") {
			t.Errorf("a real identity gets a secure timestamp: %s", c)
		}
	}
	if !signed {
		t.Errorf("pkg not signed with the installer identity: %v", r.calls)
	}
}

func TestPackageMASRejects(t *testing.T) {
	r := &fakeRunner{goos: "darwin", tools: map[string]bool{"codesign": true, "productbuild": true}}
	cases := []struct {
		name string
		mut  func(*Options)
		want string
	}{
		{"no team", func(o *Options) { o.Meta.MAS.TeamID = ""; o.MAS.AppIdentity = "Apple Distribution: A (ABCDE12345)" }, "native.bundle.macos.mas.teamId"},
		{"developer id", func(o *Options) { o.MAS.AppIdentity = "Developer ID Application: A (ABCDE12345)" }, "cannot sign for the Mac App Store"},
		{"no profile", func(o *Options) { o.MAS.AppIdentity = "Apple Distribution: A (ABCDE12345)" }, "needs a provisioning profile"},
		{"wrong profile", func(o *Options) {
			o.MAS.AppIdentity = "Apple Distribution: A (ABCDE12345)"
			o.Meta.MAS.ProvisioningProfile = fakeProfile(t, "ZZZZZ99999.com.other.app")
		}, "does not name the application identifier ABCDE12345.com.acme.notes"},
		{"not a profile", func(o *Options) {
			o.MAS.AppIdentity = "Apple Distribution: A (ABCDE12345)"
			p := filepath.Join(t.TempDir(), "x")
			os.WriteFile(p, []byte("hello"), 0o644)
			o.Meta.MAS.ProvisioningProfile = p
		}, "not a provisioning profile"},
		{"bad installer", func(o *Options) {
			o.MAS.AppIdentity = "Apple Distribution: A (ABCDE12345)"
			o.MAS.InstallerIdentity = "Developer ID Installer: A"
			o.Meta.MAS.ProvisioningProfile = fakeProfile(t, "ABCDE12345.com.acme.notes")
		}, "cannot sign a Mac App Store package"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := masOptions(t, masMeta(t), r)
			o.MAS.Getenv = func(string) string { return "" }
			tc.mut(&o)
			_, err := Package(o)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "KR7301") {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPackageMASOffMacKeepsApp(t *testing.T) {
	r := &fakeRunner{goos: "linux"}
	res, err := Package(masOptions(t, masMeta(t), r))
	if err != nil {
		t.Fatal(err)
	}
	find(t, res, "app")
	for _, a := range res.Artifacts {
		if a.Format == "mas" {
			t.Error("no pkg off macOS")
		}
	}
	if !strings.Contains(strings.Join(res.Notes, "\n"), "not signed") {
		t.Errorf("notes: %v", res.Notes)
	}
}
