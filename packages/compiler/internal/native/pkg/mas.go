package pkg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Environment variables of the Mac App Store build.
const (
	EnvMASAppIdentity       = "KRATE_MAS_APP_IDENTITY"
	EnvMASInstallerIdentity = "KRATE_MAS_INSTALLER_IDENTITY"
)

// MASOptions configure the Mac App Store variant.
type MASOptions struct {
	// AppIdentity signs the app: "3rd Party Mac Developer Application: ..." or
	// "Apple Distribution: ...". Empty (or "-") signs ad hoc, which builds a
	// sandboxed app that runs on this machine but cannot be uploaded.
	AppIdentity string
	// InstallerIdentity signs the .pkg ("3rd Party Mac Developer Installer:
	// ..." or "Mac Installer Distribution: ..."). Empty leaves it unsigned.
	InstallerIdentity string
	// Getenv reads the identities when the fields are empty; default os.Getenv.
	Getenv func(string) string
}

func (c MASOptions) env(k string) string {
	if c.Getenv != nil {
		return c.Getenv(k)
	}
	return os.Getenv(k)
}

func (c MASOptions) appIdentity() string {
	if id := first(c.AppIdentity, c.env(EnvMASAppIdentity)); id != "" {
		return id
	}
	return "-"
}

func (c MASOptions) installerIdentity() string {
	return first(c.InstallerIdentity, c.env(EnvMASInstallerIdentity))
}

// masAppIdentityOK reports whether id can sign for the Mac App Store.
func masAppIdentityOK(id string) bool {
	if id == "-" {
		return true
	}
	l := strings.ToLower(id)
	return strings.Contains(l, "3rd party mac developer application") || strings.Contains(l, "apple distribution") ||
		strings.Contains(l, "mac app distribution") ||
		// A certificate hash cannot be checked here; codesign decides.
		isHexID(id)
}

func masInstallerIdentityOK(id string) bool {
	l := strings.ToLower(id)
	return strings.Contains(l, "3rd party mac developer installer") || strings.Contains(l, "mac installer distribution") || isHexID(id)
}

func isHexID(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// MASInfoPlist is the Info.plist of the Mac App Store app: the regular one plus
// the keys App Store Connect checks.
func MASInfoPlist(m Metadata, exeName string) []byte {
	plist := InfoPlist(m, exeName)
	extra := "\t<key>CFBundleSupportedPlatforms</key>\n\t<array>\n\t\t<string>MacOSX</string>\n\t</array>\n"
	return bytes.Replace(plist, []byte("</dict>\n</plist>\n"), []byte(extra+"</dict>\n</plist>\n"), 1)
}

// checkProvisioningProfile reads a .provisionprofile (a CMS signed plist whose
// text is stored in the clear) and checks that it names the team and app id.
func checkProvisioningProfile(path string, m Metadata) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fail(diag.CodeNativeStoreConfig, path, "cannot read the provisioning profile: "+err.Error(),
			"download a Mac App Store profile for "+m.AppID+" from developer.apple.com/account/resources/profiles and set native.bundle.macos.mas.provisioningProfile")
	}
	if !bytes.Contains(data, []byte("<plist")) {
		return fail(diag.CodeNativeStoreConfig, path, "this file is not a provisioning profile", "use the .provisionprofile file downloaded from the developer portal")
	}
	want := m.MAS.TeamID + "." + m.AppID
	if !bytes.Contains(data, []byte(want)) {
		return fail(diag.CodeNativeStoreConfig, path, fmt.Sprintf("the provisioning profile does not name the application identifier %s", want),
			"create the profile for the App ID "+m.AppID+" in team "+m.MAS.TeamID+", or fix native.appId / native.bundle.macos.mas.teamId")
	}
	return nil
}

// stripUpdaterJSON removes the updater block (and the updater permission) from
// app.json, for a store build. It reports whether anything changed.
func stripUpdaterJSON(data []byte) ([]byte, bool, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, false, err
	}
	changed := false
	if _, ok := doc["updater"]; ok {
		delete(doc, "updater")
		changed = true
	}
	if raw, ok := doc["permissions"]; ok {
		var perms []string
		if json.Unmarshal(raw, &perms) == nil {
			var keep []string
			for _, p := range perms {
				if p == "updater" {
					changed = true
					continue
				}
				keep = append(keep, p)
			}
			if changed && keep != nil {
				doc["permissions"], _ = json.Marshal(keep)
			} else if changed {
				doc["permissions"] = json.RawMessage("[]")
			}
		}
	}
	if !changed {
		return data, false, nil
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	return append(out, '\n'), true, err
}

// masBundleTree builds the .app tree of the Mac App Store variant.
func masBundleTree(o *Options, ic *Icon, res *Result) (*tree, string, error) {
	m := o.Meta
	m.Store = "mas"
	o.Meta = m
	t, app, err := macAppTree(o, ic)
	if err != nil {
		return nil, "", err
	}
	exe := macExeName(m)
	t.addData(app+"/Contents/Info.plist", 0o644, MASInfoPlist(m, exe))
	if m.MAS.ProvisioningProfile != "" {
		t.addFile(app+"/Contents/embedded.provisionprofile", 0o644, m.MAS.ProvisioningProfile)
	}
	if m.Updater != nil {
		res.note("native.updater is not used in a Mac App Store build: the App Store delivers updates, so the updater block is removed from the packaged app.json")
	}
	if e, ok := t.m[app+"/Contents/Resources/app/app.json"]; ok && e.Src != "" {
		b, err := os.ReadFile(e.Src)
		if err == nil {
			if out, changed, err := stripUpdaterJSON(b); err == nil && changed {
				t.addData(e.Path, 0o644, out)
			}
		}
	}
	return t, app, nil
}

// packageMAS builds the sandboxed app, signs it and wraps it in a .pkg.
func packageMAS(o *Options, ic *Icon, stage string, res *Result) error {
	m := o.Meta
	adHoc := o.MAS.appIdentity() == "-"
	if m.MAS.TeamID == "" && !adHoc {
		return fail(diag.CodeNativeStoreConfig, "native.bundle.macos.mas.teamId", "a Mac App Store build needs the Apple team id",
			"set native.bundle.macos.mas.teamId (developer.apple.com/account, Membership details)")
	}
	if !masAppIdentityOK(o.MAS.appIdentity()) {
		return fail(diag.CodeNativeStoreConfig, EnvMASAppIdentity, fmt.Sprintf("%q cannot sign for the Mac App Store", o.MAS.appIdentity()),
			`use a "3rd Party Mac Developer Application: Name (TEAMID)" or "Apple Distribution: Name (TEAMID)" identity; Developer ID identities are for apps outside the store`)
	}
	if id := o.MAS.installerIdentity(); id != "" && !masInstallerIdentityOK(id) {
		return fail(diag.CodeNativeStoreConfig, EnvMASInstallerIdentity, fmt.Sprintf("%q cannot sign a Mac App Store package", id),
			`use a "3rd Party Mac Developer Installer: Name (TEAMID)" or "Mac Installer Distribution: Name (TEAMID)" identity`)
	}
	switch {
	case m.MAS.ProvisioningProfile != "":
		if !adHoc || m.MAS.TeamID != "" {
			if err := checkProvisioningProfile(m.MAS.ProvisioningProfile, m); err != nil {
				return err
			}
		}
	case !adHoc:
		return fail(diag.CodeNativeStoreConfig, "native.bundle.macos.mas.provisioningProfile", "a Mac App Store build needs a provisioning profile",
			"create a Mac App Store profile for "+m.AppID+" at developer.apple.com/account/resources/profiles and set native.bundle.macos.mas.provisioningProfile")
	default:
		res.note("no provisioning profile: this ad hoc signed sandboxed app runs here but cannot be uploaded to App Store Connect")
	}
	t, app, err := masBundleTree(o, ic, res)
	if err != nil {
		return err
	}
	if err := writeDisk(stage, t); err != nil {
		return wrapWrite(stage, err)
	}
	appPath := filepath.Join(stage, app)
	res.add(Artifact{Path: appPath, Format: "app", Detail: "Mac App Store variant"})

	if err := signMAS(o, appPath, adHoc); err != nil {
		if pe, ok := err.(*Error); ok && pe.Diag.Code == diag.CodeNativeSignTool {
			res.note("the app is not signed: %s", firstLine(pe.Diag.Message))
			return nil
		}
		return err
	}
	return makeMASPkg(o, appPath, res)
}

// masEntitlementsFor returns the main entitlements; an ad hoc build leaves out
// the restricted identifiers, which need a provisioning profile.
func masEntitlementsFor(m Metadata, adHoc bool) ([]plistEntry, error) {
	var out []plistEntry
	for _, e := range masEntitlements(m) {
		if adHoc && (e.Key == "com.apple.application-identifier" || e.Key == "com.apple.developer.team-identifier") {
			continue
		}
		out = append(out, e)
	}
	if m.MAS.Entitlements != "" {
		b, err := os.ReadFile(m.MAS.Entitlements)
		if err != nil {
			return nil, fail(diag.CodeNativeStoreConfig, m.MAS.Entitlements, "cannot read the extra entitlements: "+err.Error(), "check native.bundle.macos.mas.entitlements")
		}
		out = append(out, extraEntitlements(b)...)
	}
	return out, nil
}

// extraEntitlements reads the boolean-true keys of a user entitlements plist.
func extraEntitlements(b []byte) []plistEntry {
	var out []plistEntry
	s := string(b)
	for {
		i := strings.Index(s, "<key>")
		if i < 0 {
			return out
		}
		j := strings.Index(s[i:], "</key>")
		if j < 0 {
			return out
		}
		key := strings.TrimSpace(s[i+5 : i+j])
		rest := strings.TrimSpace(s[i+j+6:])
		s = rest
		switch {
		case strings.HasPrefix(rest, "<true/>"):
			out = append(out, plistEntry{Key: key})
		case strings.HasPrefix(rest, "<string>"):
			if k := strings.Index(rest, "</string>"); k > 0 {
				out = append(out, plistEntry{Key: key, Str: rest[len("<string>"):k]})
			}
		}
	}
}

// signMAS signs nested code with the inherit entitlements and the app with the
// full sandbox entitlements. The hardened runtime is not used: the store
// applies its own checks.
func signMAS(o *Options, app string, adHoc bool) error {
	sc := SignConfig{Runner: o.Runner, Log: o.Log}
	if err := needCodesign(&sc); err != nil {
		return err
	}
	main, err := masEntitlementsFor(o.Meta, adHoc)
	if err != nil {
		return err
	}
	dir := filepath.Dir(app)
	mainPlist := filepath.Join(dir, "mas-app.entitlements")
	jitPlist := filepath.Join(dir, "mas-child-jit.entitlements")
	childPlist := filepath.Join(dir, "mas-child.entitlements")
	for p, e := range map[string][]plistEntry{
		mainPlist:  main,
		jitPlist:   masChildEntitlements(o.Meta, true),
		childPlist: masChildEntitlements(o.Meta, false),
	} {
		if err := os.WriteFile(p, entitlementsPlist(e), 0o644); err != nil {
			return wrapWrite(p, err)
		}
	}
	id := o.MAS.appIdentity()
	ts := "--timestamp"
	if adHoc {
		ts = "--timestamp=none"
	}
	nested, err := nestedCode(app)
	if err != nil {
		return wrapWrite(app, err)
	}
	for _, p := range nested {
		ent := childPlist
		if base := filepath.Base(p); strings.HasPrefix(base, "krate-host-") && base != "krate-host-scriptc" && base != "krate-host-go" {
			ent = jitPlist
		}
		args := []string{"--force", ts, "--entitlements", ent, "--sign", id, p}
		if out, err := o.run("", nil, "codesign", args...); err != nil {
			return sc.signFailed(p, out, err)
		}
	}
	if out, err := o.run("", nil, "codesign", "--force", ts, "--entitlements", mainPlist, "--sign", id, app); err != nil {
		return sc.signFailed(app, out, err)
	}
	if out, err := o.run("", nil, "codesign", "--verify", "--deep", "--strict", "--verbose=2", app); err != nil {
		return sc.signFailed(app, out, err)
	}
	return nil
}

// makeMASPkg wraps the app in an installer package with productbuild.
func makeMASPkg(o *Options, app string, res *Result) error {
	out := filepath.Join(o.OutDir, artifactName(o.Meta, o.Payload, "-mas.pkg"))
	if o.Runner.GOOS() != "darwin" {
		res.note("no .pkg: productbuild only runs on macOS; the signed .app is in %s", filepath.Dir(app))
		return nil
	}
	if _, err := o.need("productbuild", "mas", "productbuild ships with the Xcode command line tools (xcode-select --install)"); err != nil {
		res.note("no .pkg: productbuild was not found")
		return nil
	}
	args := []string{"--component", app, "/Applications"}
	if id := o.MAS.installerIdentity(); id != "" {
		args = append(args, "--sign", id)
	} else {
		res.note("the .pkg is unsigned: set %s (or --installer-identity) to the \"3rd Party Mac Developer Installer\" identity to upload it", EnvMASInstallerIdentity)
	}
	args = append(args, out)
	_ = os.Remove(out)
	if b, err := o.run("", nil, "productbuild", args...); err != nil {
		return fail(diag.CodeNativeSignFailed, out, "productbuild failed:\n"+indentLines(string(b)), "run the command by hand to see the error")
	}
	res.add(Artifact{Path: out, Format: "mas", Detail: "upload with Transporter or xcrun altool"})
	return nil
}
