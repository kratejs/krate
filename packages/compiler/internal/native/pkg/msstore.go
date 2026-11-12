package pkg

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

var storePublisherRe = regexp.MustCompile(`^CN=[^,=]+(,\s*[A-Za-z]+=[^,]+)*$`)

// validateStoreMeta checks the Partner Center identity of a Store build.
func validateStoreMeta(m Metadata) error {
	s := m.WinStore
	missing := func(key, what string) error {
		return fail(diag.CodeNativeStoreConfig, "native.bundle.windows.store."+key, "a Microsoft Store build needs "+what,
			"copy it from Partner Center: your app, Product management, Product identity; set native.bundle.windows.store.{identityName,publisherId,publisherDisplayName}")
	}
	switch {
	case s.IdentityName == "":
		return missing("identityName", "the package identity name")
	case s.PublisherID == "":
		return missing("publisherId", "the publisher id")
	case s.PublisherDisplayName == "":
		return missing("publisherDisplayName", "the publisher display name")
	}
	if !storePublisherRe.MatchString(s.PublisherID) {
		return fail(diag.CodeNativeStoreConfig, "native.bundle.windows.store.publisherId", fmt.Sprintf("%q is not a publisher distinguished name", s.PublisherID),
			`Partner Center shows it as "CN=" followed by a GUID`)
	}
	return nil
}

// packageMSIXStore writes the Store variant of the MSIX package and the
// .msixupload that Partner Center takes. It is unsigned: the Store signs the
// package it publishes.
func packageMSIXStore(o *Options, ic *Icon, stage, dir string, res *Result) error {
	if err := validateStoreMeta(o.Meta); err != nil {
		return err
	}
	m, p := o.Meta, o.Payload
	m.Store = "msstore"
	o.Meta = m
	inner := newTree()
	if err := inner.addDisk("", filepath.Join(stage, dir)); err != nil {
		return wrapWrite(stage, err)
	}
	if m.Updater != nil {
		res.note("native.updater is not used in a Microsoft Store build: the Store delivers updates, so the updater block is removed from the packaged app.json")
	}
	if e, ok := inner.m["app/app.json"]; ok && e.Src != "" {
		if b, err := os.ReadFile(e.Src); err == nil {
			if out, changed, err := stripUpdaterJSON(b); err == nil && changed {
				inner.addData(e.Path, 0o644, out)
			}
		}
	}
	inner.addData(distMarkerName, 0o644, []byte(`{ "store": "msstore" }`+"\n"))
	out := filepath.Join(o.OutDir, artifactName(m, p, "-store.msix"))
	if err := BuildMSIX(m, p, ic, inner, out); err != nil {
		return err
	}
	if problems := CheckStoreMSIX(out, m); len(problems) > 0 {
		return fail(diag.CodeNativeStoreConfig, out, "the package breaks Microsoft Store rules:\n"+indentLines(strings.Join(problems, "\n")), "fix the items above; the package is kept for inspection")
	}
	res.add(Artifact{Path: out, Format: "msix-store", Detail: "unsigned; the Store signs it"})
	up := filepath.Join(o.OutDir, fmt.Sprintf("%s-%s-windows-%s.msixupload", m.slug(), m.Version, p.Arch))
	if err := writeUpload(up, []string{out}); err != nil {
		return wrapWrite(up, err)
	}
	res.add(Artifact{Path: up, Format: "msixupload", Detail: "upload in Partner Center, Packages"})
	return nil
}

const distMarkerName = "krate-dist.json"

// writeUpload writes a .msixupload: a zip that holds the package or bundle.
func writeUpload(out string, files []string) error {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		w, err := zw.Create(filepath.Base(f))
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return writeFileAtomic(out, buf.Bytes())
}

// CheckStoreMSIX reads a package back and reports what the Windows App
// Certification Kit or Partner Center would reject for a full-trust desktop
// app, as far as it can be seen without Windows: the identity must be the
// Partner Center one, the executable and the required logos must be there, the
// only restricted capability is runFullTrust, the version ends in .0, and
// nothing in the package may update itself.
func CheckStoreMSIX(path string, m Metadata) []string {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return []string{"cannot read the package: " + err.Error()}
	}
	defer zr.Close()
	files := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		name := f.Name
		files[name] = b
	}
	var probs []string
	man, ok := files["AppxManifest.xml"]
	if !ok {
		return []string{"AppxManifest.xml is missing"}
	}
	var doc struct {
		Identity struct {
			Name      string `xml:"Name,attr"`
			Publisher string `xml:"Publisher,attr"`
			Version   string `xml:"Version,attr"`
		} `xml:"Identity"`
		Props struct {
			Display  string `xml:"PublisherDisplayName"`
			Logo     string `xml:"Logo"`
			DispName string `xml:"DisplayName"`
		} `xml:"Properties"`
		Apps []struct {
			Executable string `xml:"Executable,attr"`
			EntryPoint string `xml:"EntryPoint,attr"`
		} `xml:"Applications>Application"`
		Caps []struct {
			Name string `xml:"Name,attr"`
		} `xml:"Capabilities>Capability"`
	}
	if err := xml.Unmarshal(man, &doc); err != nil {
		return []string{"AppxManifest.xml is not valid XML: " + err.Error()}
	}
	s := m.WinStore
	if doc.Identity.Name != s.IdentityName || doc.Identity.Publisher != s.PublisherID {
		probs = append(probs, fmt.Sprintf("the identity %q / %q does not match Partner Center (%q / %q)", doc.Identity.Name, doc.Identity.Publisher, s.IdentityName, s.PublisherID))
	}
	if doc.Props.Display != s.PublisherDisplayName {
		probs = append(probs, "PublisherDisplayName does not match Partner Center")
	}
	if !strings.HasSuffix(doc.Identity.Version, ".0") {
		probs = append(probs, fmt.Sprintf("the version %s must end in .0: the Store reserves the fourth part", doc.Identity.Version))
	}
	if len(doc.Apps) == 0 {
		probs = append(probs, "the manifest declares no application")
	}
	for _, a := range doc.Apps {
		if _, ok := files[partName(a.Executable)]; !ok {
			if _, ok := files[a.Executable]; !ok {
				probs = append(probs, "the executable "+a.Executable+" is not in the package")
			}
		}
		if a.EntryPoint != "Windows.FullTrustApplication" {
			probs = append(probs, "EntryPoint must be Windows.FullTrustApplication for a desktop app")
		}
	}
	hasFull := false
	for _, c := range doc.Caps {
		if c.Name == "runFullTrust" {
			hasFull = true
		}
	}
	if !hasFull {
		probs = append(probs, "a desktop app needs the runFullTrust capability")
	}
	for _, logo := range msixLogos {
		if _, ok := files[logo.Name]; !ok {
			probs = append(probs, "the logo "+logo.Name+" is missing")
		}
	}
	for name, data := range files {
		lower := strings.ToLower(name)
		if strings.HasSuffix(lower, "app/app.json") && bytes.Contains(data, []byte(`"updater"`)) {
			probs = append(probs, name+" still configures the self-updater, which the Store forbids")
		}
		if strings.Contains(lower, "update.exe") || strings.HasSuffix(lower, "squirrel.exe") {
			probs = append(probs, name+" is a self-updater, which the Store forbids")
		}
	}
	sort.Strings(probs)
	return probs
}

// bundledPackage is one MSIX in a bundle.
type bundledPackage struct {
	Arch string // "x64" or "arm64"
	Path string
}

// BuildMSIXBundle writes an .msixbundle holding the packages (the same
// application for different architectures). Packages are stored uncompressed,
// as the format requires, and the bundle carries its own manifest and block
// map. It is unsigned.
func BuildMSIXBundle(m Metadata, pkgs []bundledPackage, out string) error {
	if len(pkgs) == 0 {
		return fmt.Errorf("a bundle needs at least one package")
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Arch < pkgs[j].Arch })
	v, _ := splitVersion(m.Version)
	ident, pub := msixIdentityName(m.AppID), msixPublisher(m.Publisher)
	if m.Store == "msstore" {
		ident, pub = m.WinStore.IdentityName, m.WinStore.PublisherID
	}
	type entry struct {
		name string
		data []byte
	}
	var pk []entry
	for _, p := range pkgs {
		data, err := os.ReadFile(p.Path)
		if err != nil {
			return err
		}
		pk = append(pk, entry{fmt.Sprintf("%s_%s_%s.msix", ident, v.msix(), p.Arch), data})
	}
	// The manifest records where each package's data starts in the zip, so
	// lay the zip out first: [Content_Types].xml, the packages in order, then
	// the manifest and the block map (which sit after them and need no offset).
	offsets := make([]int64, len(pk))
	pos := int64(0)
	ct := bundleContentTypes()
	pos += 30 + int64(len(partName("[Content_Types].xml"))) + int64(len(ct))
	for i, e := range pk {
		offsets[i] = pos + 30 + int64(len(partName(e.name)))
		pos = offsets[i] + int64(len(e.data))
	}
	var man strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&man, format+"\n", args...) }
	w(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	w(`<Bundle xmlns="http://schemas.microsoft.com/appx/2013/bundle" SchemaVersion="6.0.0.0">`)
	w(`  <Identity Name="%s" Publisher="%s" Version="%s"/>`, xmlEscape(ident), xmlEscape(pub), v.msix())
	w(`  <Packages>`)
	for i, p := range pkgs {
		w(`    <Package Type="application" Version="%s" Architecture="%s" FileName="%s" Offset="%d" Size="%d">`, v.msix(), p.Arch, xmlEscape(pk[i].name), offsets[i], len(pk[i].data))
		w(`      <Resources>`)
		w(`        <Resource Language="en-us"/>`)
		w(`      </Resources>`)
		w(`    </Package>`)
	}
	w(`  </Packages>`)
	w(`</Bundle>`)

	var infos []blockInfo
	for _, e := range pk {
		infos = append(infos, blockInfo{Name: e.name, Size: len(e.data), LfhSize: 30 + len(partName(e.name)), Hashes: blockHashes(e.data)})
	}
	bm := BlockMapXML(infos)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := writeStored(zw, "[Content_Types].xml", ct); err != nil {
		return err
	}
	for _, e := range pk {
		if err := writeStored(zw, partName(e.name), e.data); err != nil {
			return err
		}
	}
	if err := writeStored(zw, "AppxMetaData/AppxBundleManifest.xml", []byte(man.String())); err != nil {
		return err
	}
	if err := writeStored(zw, "AppxBlockMap.xml", bm); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return writeFileAtomic(out, buf.Bytes())
}

func bundleContentTypes() []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` + "\n" +
		`  <Default Extension="msix" ContentType="application/vnd.ms-appx"/>` + "\n" +
		`  <Override PartName="/AppxMetaData/AppxBundleManifest.xml" ContentType="application/vnd.ms-appx.bundlemanifest+xml"/>` + "\n" +
		`  <Override PartName="/AppxBlockMap.xml" ContentType="application/vnd.ms-appx.blockmap+xml"/>` + "\n" +
		`</Types>` + "\n")
}

// PackageStoreBundle builds the Store MSIX for x64 and arm64 and combines them
// into an .msixbundle and a .msixupload. The two payloads are the Windows
// builds of the same app.
func PackageStoreBundle(o Options, x64, arm64 Payload) (*Result, error) {
	o.Formats = []string{"msix-store"}
	o.UpdaterKey = nil
	res := &Result{}
	var pkgs []bundledPackage
	for _, pl := range []struct {
		arch string
		p    Payload
	}{{"x64", x64}, {"arm64", arm64}} {
		oo := o
		oo.Payload = pl.p
		r, err := Package(oo)
		if err != nil {
			return nil, err
		}
		res.Notes = append(res.Notes, r.Notes...)
		for _, a := range r.Artifacts {
			if a.Format == "msix-store" {
				pkgs = append(pkgs, bundledPackage{Arch: pl.arch, Path: a.Path})
			}
			if a.Format == "zip" || a.Format == "msix-store" {
				res.add(a)
			}
		}
	}
	m := o.Meta
	m.Store = "msstore"
	bundle := filepath.Join(o.OutDir, fmt.Sprintf("%s-%s-windows-universal.msixbundle", m.slug(), m.Version))
	if err := BuildMSIXBundle(m, pkgs, bundle); err != nil {
		return nil, wrapWrite(bundle, err)
	}
	res.add(Artifact{Path: bundle, Format: "msixbundle", Detail: "x64 and arm64; unsigned"})
	up := filepath.Join(o.OutDir, fmt.Sprintf("%s-%s-windows-universal.msixupload", m.slug(), m.Version))
	if err := writeUpload(up, []string{bundle}); err != nil {
		return nil, wrapWrite(up, err)
	}
	res.add(Artifact{Path: up, Format: "msixupload", Detail: "upload in Partner Center, Packages"})
	res.Stage = filepath.Join(o.OutDir, "windows-universal")
	sort.SliceStable(res.Artifacts, func(i, j int) bool { return res.Artifacts[i].Path < res.Artifacts[j].Path })
	return res, nil
}
