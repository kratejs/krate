package pkg

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// minMacOS is the oldest macOS the Go toolchain used here supports.
const minMacOS = "12.0"

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s)) // a bytes.Buffer never fails
	return b.String()
}

// InfoPlist returns the Contents/Info.plist of the app.
func InfoPlist(m Metadata, exeName string) []byte {
	v, _ := splitVersion(m.Version)
	var b strings.Builder
	str := func(k, val string) {
		fmt.Fprintf(&b, "\t<key>%s</key>\n\t<string>%s</string>\n", k, xmlEscape(val))
	}
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	str("CFBundleDevelopmentRegion", "en")
	str("CFBundleExecutable", exeName)
	str("CFBundleIconFile", "icon")
	str("CFBundleIdentifier", m.AppID)
	str("CFBundleInfoDictionaryVersion", "6.0")
	str("CFBundleName", m.ProductName)
	str("CFBundleDisplayName", m.ProductName)
	str("CFBundlePackageType", "APPL")
	str("CFBundleShortVersionString", v.core())
	str("CFBundleVersion", v.core())
	str("LSApplicationCategoryType", m.MacCategory)
	str("LSMinimumSystemVersion", minMacOS)
	b.WriteString("\t<key>NSHighResolutionCapable</key>\n\t<true/>\n")
	str("NSPrincipalClass", "NSApplication")
	b.WriteString("\t<key>NSSupportsAutomaticGraphicsSwitching</key>\n\t<true/>\n")
	for _, kv := range macUsageKeys(m) {
		str(kv[0], kv[1])
	}
	if len(m.DeepLinks) > 0 {
		links := append([]string(nil), m.DeepLinks...)
		sort.Strings(links)
		b.WriteString("\t<key>CFBundleURLTypes</key>\n\t<array>\n\t\t<dict>\n")
		b.WriteString("\t\t\t<key>CFBundleTypeRole</key>\n\t\t\t<string>Viewer</string>\n")
		fmt.Fprintf(&b, "\t\t\t<key>CFBundleURLName</key>\n\t\t\t<string>%s</string>\n", xmlEscape(m.AppID))
		b.WriteString("\t\t\t<key>CFBundleURLSchemes</key>\n\t\t\t<array>\n")
		for _, s := range links {
			fmt.Fprintf(&b, "\t\t\t\t<string>%s</string>\n", xmlEscape(s))
		}
		b.WriteString("\t\t\t</array>\n\t\t</dict>\n\t</array>\n")
	}
	b.WriteString("</dict>\n</plist>\n")
	return []byte(b.String())
}

func macExeName(m Metadata) string { return fileSafe(m.ProductName) }

// macAppTree lays out the .app bundle (paths relative to the directory that
// holds the bundle).
func macAppTree(o *Options, ic *Icon) (*tree, string, error) {
	m := o.Meta
	app := fileSafe(m.ProductName) + ".app"
	exe := macExeName(m)
	t := newTree()
	t.addFile(app+"/Contents/MacOS/"+exe, 0o755, o.Payload.Executable)
	t.addData(app+"/Contents/Info.plist", 0o644, InfoPlist(m, exe))
	t.addData(app+"/Contents/PkgInfo", 0o644, []byte("APPL????"))
	icns, err := ic.ICNS()
	if err != nil {
		return nil, "", fail(diag.CodeNativePkgIcon, "native.icons", err.Error(), "")
	}
	t.addData(app+"/Contents/Resources/icon.icns", 0o644, icns)
	for dest, src := range o.Payload.ExtraFiles {
		if err := t.addDisk(app+"/Contents/Resources/"+cleanPath(dest), src); err != nil {
			return nil, "", fail(diag.CodeNativePkgInput, src, err.Error(), "")
		}
	}
	return t, app, nil
}

func packageMac(o *Options, ic *Icon, want map[string]bool, stage string, res *Result) error {
	p := o.Payload
	t, app, err := macAppTree(o, ic)
	if err != nil {
		return err
	}
	if err := writeDisk(stage, t); err != nil {
		return wrapWrite(stage, err)
	}
	if p.Arch == "arm64" {
		if ok, err := machoHasSignature(p.Executable); err == nil && !ok {
			res.note("%s has no code signature; Apple silicon refuses to run unsigned code. Build it with the Go linker for darwin/arm64 (it signs ad hoc) or run krate native sign", filepath.Base(p.Executable))
		}
	}
	if o.Signer != nil {
		if err := o.Signer.SignStage(stage); err != nil {
			return err
		}
	} else {
		res.note("the .app has an ad hoc signature on its executable only; sign the bundle with `krate native sign` on macOS (codesign) for Gatekeeper")
	}
	res.add(Artifact{Path: filepath.Join(stage, app), Format: "app"})
	var staged *tree
	if o.Signer == nil {
		staged = t
	}
	return macArchives(o, want, stage, app, res, staged)
}

// macArchives builds the update zip and the dmg from the staged .app. When
// nothing signed the stage, built is the tree that was written to it and is
// archived as is: a Windows host cannot read modes back from disk.
func macArchives(o *Options, want map[string]bool, stage, app string, res *Result, built *tree) error {
	m, p := o.Meta, o.Payload

	signed := built
	if signed == nil {
		// Reload so the archives carry what signing wrote.
		signed = newTree()
		if err := signed.addDisk("", stage); err != nil {
			return wrapWrite(stage, err)
		}
	}
	zipPath := filepath.Join(o.OutDir, artifactName(m, p, ".zip"))
	if err := writeZipFile(zipPath, signed, ""); err != nil {
		return wrapWrite(zipPath, err)
	}
	res.add(Artifact{Path: zipPath, Format: "zip", Update: true})

	if want["dmg"] {
		dmg := filepath.Join(o.OutDir, artifactName(m, p, ".dmg"))
		if err := makeDMG(o, stage, app, dmg); err != nil {
			if pe, ok := err.(*Error); ok && pe.Diag.Code == diag.CodeNativePkgTool {
				res.note("no .dmg: %s; delivering the .app as %s instead", firstLine(pe.Diag.Message), filepath.Base(zipPath))
			} else {
				return err
			}
		} else {
			res.add(Artifact{Path: dmg, Format: "dmg"})
			if o.Signer != nil {
				if err := o.Signer.SignFile(dmg, "dmg"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func writeZipFile(p string, t *tree, prefix string) error {
	f, err := createFile(p)
	if err != nil {
		return err
	}
	if err := writeZip(f, t, prefix); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// makeDMG builds a compressed disk image with hdiutil (macOS only).
func makeDMG(o *Options, stage, app, out string) error {
	if o.Runner.GOOS() != "darwin" {
		return fail(diag.CodeNativePkgTool, "--format dmg", "a .dmg can only be made on macOS (hdiutil)", "package on a Mac, or distribute the .zip")
	}
	if _, err := o.need("hdiutil", "dmg", "hdiutil ships with macOS"); err != nil {
		return err
	}
	src, err := os.MkdirTemp(filepath.Dir(out), ".dmg-src-")
	if err != nil {
		return wrapWrite(out, err)
	}
	defer os.RemoveAll(src)
	t := newTree()
	if err := t.addDisk(app, filepath.Join(stage, app)); err != nil {
		return wrapWrite(stage, err)
	}
	t.addSymlink("Applications", "/Applications")
	if err := writeDisk(src, t); err != nil {
		return wrapWrite(src, err)
	}
	os.Remove(out)
	if b, err := o.run("", nil, "hdiutil", "create", "-volname", o.Meta.ProductName, "-srcfolder", src, "-ov", "-format", "UDZO", out); err != nil {
		return fail(diag.CodeNativeSignFailed, out, "hdiutil failed: "+strings.TrimSpace(string(b)), "run the command by hand to see the error")
	}
	return nil
}

// MacAppName is the file name of the app bundle.
func MacAppName(m Metadata) string { return fileSafe(m.ProductName) + ".app" }
