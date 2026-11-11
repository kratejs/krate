package pkg

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"hash/crc32"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// blockSize is the unit of the MSIX block map.
const blockSize = 64 * 1024

var msixNameRe = strings.NewReplacer("_", "-")

func msixIdentityName(appID string) string {
	n := msixNameRe.Replace(appID)
	if len(n) > 50 {
		n = n[:50]
	}
	n = strings.TrimRight(n, ".-")
	for len(n) < 3 {
		n += "x"
	}
	return n
}

// msixPublisher returns the Publisher attribute: a distinguished name that
// must equal the subject of the signing certificate.
func msixPublisher(p string) string {
	if strings.Contains(p, "=") {
		return p
	}
	return "CN=" + p
}

func publisherDisplay(p string) string {
	p = msixPublisher(p)
	for _, part := range strings.Split(p, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToUpper(part), "CN=") {
			return strings.Trim(part[3:], "\"")
		}
	}
	return p
}

// msixLogos lists the generated logo files and their pixel sizes.
var msixLogos = []struct {
	Name string
	Size int
}{
	{"Assets/StoreLogo.png", 50},
	{"Assets/Square44x44Logo.png", 44},
	{"Assets/Square150x150Logo.png", 150},
}

// AppxManifest returns AppxManifest.xml.
func AppxManifest(m Metadata, p Payload) []byte {
	v, _ := splitVersion(m.Version)
	arch := "x64"
	if p.Arch == "arm64" {
		arch = "arm64"
	}
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	w(`<?xml version="1.0" encoding="UTF-8"?>`)
	w(`<Package xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10"`)
	w(`         xmlns:uap="http://schemas.microsoft.com/appx/manifest/uap/windows10"`)
	w(`         xmlns:rescap="http://schemas.microsoft.com/appx/manifest/foundation/windows10/restrictedcapabilities"`)
	w(`         IgnorableNamespaces="uap rescap">`)
	w(`  <Identity Name="%s" Publisher="%s" Version="%s" ProcessorArchitecture="%s"/>`,
		xmlEscape(msixIdentityName(m.AppID)), xmlEscape(msixPublisher(m.Publisher)), v.msix(), arch)
	w(`  <Properties>`)
	w(`    <DisplayName>%s</DisplayName>`, xmlEscape(m.ProductName))
	w(`    <PublisherDisplayName>%s</PublisherDisplayName>`, xmlEscape(publisherDisplay(m.Publisher)))
	w(`    <Logo>Assets\StoreLogo.png</Logo>`)
	w(`  </Properties>`)
	w(`  <Dependencies>`)
	w(`    <TargetDeviceFamily Name="Windows.Desktop" MinVersion="10.0.17763.0" MaxVersionTested="10.0.22631.0"/>`)
	w(`  </Dependencies>`)
	w(`  <Resources>`)
	w(`    <Resource Language="en-us"/>`)
	w(`  </Resources>`)
	w(`  <Applications>`)
	w(`    <Application Id="App" Executable="%s" EntryPoint="Windows.FullTrustApplication">`, xmlEscape(winExeName(m)))
	w(`      <uap:VisualElements DisplayName="%s" Description="%s" BackgroundColor="transparent"`, xmlEscape(m.ProductName), xmlEscape(m.ProductName))
	w(`                          Square150x150Logo="Assets\Square150x150Logo.png" Square44x44Logo="Assets\Square44x44Logo.png"/>`)
	if len(m.DeepLinks) > 0 {
		w(`      <Extensions>`)
		for _, s := range m.DeepLinks {
			w(`        <uap:Extension Category="windows.protocol">`)
			w(`          <uap:Protocol Name="%s">`, xmlEscape(s))
			w(`            <uap:DisplayName>%s</uap:DisplayName>`, xmlEscape(m.ProductName))
			w(`          </uap:Protocol>`)
			w(`        </uap:Extension>`)
		}
		w(`      </Extensions>`)
	}
	w(`    </Application>`)
	w(`  </Applications>`)
	w(`  <Capabilities>`)
	w(`    <rescap:Capability Name="runFullTrust"/>`)
	w(`  </Capabilities>`)
	w(`</Package>`)
	return []byte(b.String())
}

// contentTypes maps file extensions to OPC content types.
var contentTypes = map[string]string{
	"png": "image/png", "exe": "application/x-msdownload", "dll": "application/x-msdownload",
	"json": "application/json", "js": "text/javascript", "html": "text/html", "css": "text/css",
	"svg": "image/svg+xml", "xml": "application/xml", "txt": "text/plain", "ico": "image/x-icon",
	"woff2": "font/woff2", "woff": "font/woff", "map": "application/json", "wasm": "application/wasm",
}

// ContentTypesXML returns [Content_Types].xml for the given part names.
func ContentTypesXML(parts []string) []byte {
	exts := map[string]string{}
	var overrides []string
	for _, p := range parts {
		ext := strings.ToLower(strings.TrimPrefix(path.Ext(p), "."))
		if ext == "" {
			overrides = append(overrides, p)
			continue
		}
		ct := contentTypes[ext]
		if ct == "" {
			ct = "application/octet-stream"
		}
		exts[ext] = ct
	}
	names := make([]string, 0, len(exts))
	for e := range exts {
		names = append(names, e)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` + "\n")
	for _, e := range names {
		fmt.Fprintf(&b, `  <Default Extension="%s" ContentType="%s"/>`+"\n", xmlEscape(e), exts[e])
	}
	for _, p := range overrides {
		fmt.Fprintf(&b, `  <Override PartName="/%s" ContentType="application/octet-stream"/>`+"\n", xmlEscape(p))
	}
	b.WriteString(`  <Override PartName="/AppxManifest.xml" ContentType="application/vnd.ms-appx.manifest+xml"/>` + "\n")
	b.WriteString(`  <Override PartName="/AppxBlockMap.xml" ContentType="application/vnd.ms-appx.blockmap+xml"/>` + "\n")
	b.WriteString(`</Types>` + "\n")
	return []byte(b.String())
}

// partName percent-encodes a package part name as MSIX stores it.
func partName(p string) string {
	if p == "[Content_Types].xml" {
		return p
	}
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '.', c == '_', c == '~', c == '/', c == '!', c == '$', c == '&', c == '\'', c == '(', c == ')', c == '*', c == '+', c == ',', c == ';', c == '=', c == '@':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

type msixFile struct {
	name string // slash path, unencoded
	data []byte
}

// blockInfo describes one file in AppxBlockMap.xml.
type blockInfo struct {
	Name    string // backslash path
	Size    int
	LfhSize int
	Hashes  []string
}

func blockHashes(data []byte) []string {
	var out []string
	for off := 0; off < len(data); off += blockSize {
		end := min(off+blockSize, len(data))
		sum := sha256.Sum256(data[off:end])
		out = append(out, base64.StdEncoding.EncodeToString(sum[:]))
	}
	return out
}

// BlockMapXML renders AppxBlockMap.xml: for every file its size, the size of
// its local zip header and the SHA-256 (base64) of each 64 KiB block. Files
// are stored without compression, so blocks carry no Size attribute.
func BlockMapXML(files []blockInfo) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<BlockMap xmlns="http://schemas.microsoft.com/appx/2010/blockmap" HashMethod="http://www.w3.org/2001/04/xmlenc#sha256">` + "\n")
	for _, f := range files {
		fmt.Fprintf(&b, `  <File Name="%s" Size="%d" LfhSize="%d">`, xmlEscape(f.Name), f.Size, f.LfhSize)
		if len(f.Hashes) == 0 {
			b.WriteString("</File>\n")
			continue
		}
		b.WriteString("\n")
		for _, h := range f.Hashes {
			fmt.Fprintf(&b, `    <Block Hash="%s"/>`+"\n", h)
		}
		b.WriteString("  </File>\n")
	}
	b.WriteString("</BlockMap>\n")
	return []byte(b.String())
}

// BuildMSIX writes an unsigned MSIX package. inner holds the application
// files at their install paths (the executable at the root). Signing is left
// to signtool.
func BuildMSIX(m Metadata, p Payload, ic *Icon, inner *tree, out string) error {
	if m.Publisher == "" {
		return fail(diag.CodeNativePkgMeta, "native.bundle.windows.publisher", "an MSIX package needs a publisher",
			`set native.bundle.windows.publisher to the subject of your signing certificate, for example "CN=Acme Inc"`)
	}
	var files []msixFile
	for _, e := range inner.sorted() {
		if e.isDir() {
			continue
		}
		if e.isSymlink() {
			return fail(diag.CodeNativePkgInput, e.Path, "an MSIX package cannot hold a symlink", "remove it from the app files")
		}
		r, err := e.open()
		if err != nil {
			return wrapWrite(out, err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			return wrapWrite(out, err)
		}
		files = append(files, msixFile{e.Path, data})
	}
	for _, l := range msixLogos {
		data, err := ic.PNG(l.Size)
		if err != nil {
			return fail(diag.CodeNativePkgIcon, "native.icons", err.Error(), "")
		}
		files = append(files, msixFile{l.Name, data})
	}
	files = append(files, msixFile{"AppxManifest.xml", AppxManifest(m, p)})
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })

	parts := make([]string, len(files))
	for i, f := range files {
		parts[i] = f.name
	}
	ct := msixFile{"[Content_Types].xml", ContentTypesXML(parts)}

	var infos []blockInfo
	for _, f := range files {
		infos = append(infos, blockInfo{
			Name: strings.ReplaceAll(f.name, "/", `\`), Size: len(f.data),
			LfhSize: 30 + len(partName(f.name)), Hashes: blockHashes(f.data),
		})
	}
	bm := msixFile{"AppxBlockMap.xml", BlockMapXML(infos)}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range append([]msixFile{ct}, append(files, bm)...) {
		if err := writeStored(zw, partName(f.name), f.data); err != nil {
			return wrapWrite(out, err)
		}
	}
	if err := zw.Close(); err != nil {
		return wrapWrite(out, err)
	}
	return wrapWrite(out, writeFileAtomic(out, buf.Bytes()))
}

// writeStored adds an uncompressed entry with known CRC and sizes, so the
// local header has no data descriptor and no extra fields.
func writeStored(zw *zip.Writer, name string, data []byte) error {
	h := &zip.FileHeader{
		Name: name, Method: zip.Store,
		// Windows accepts only version 10 (or 45 for zip64) here; Go's raw
		// writer would leave 0, which makeappx rejects as inconsistent.
		CreatorVersion: 10, ReaderVersion: 10,
		CRC32:              crc32.ChecksumIEEE(data),
		CompressedSize64:   uint64(len(data)),
		UncompressedSize64: uint64(len(data)),
		ModifiedDate:       0x0021, // 1980-01-01
		ModifiedTime:       0,
	}
	w, err := zw.CreateRaw(h)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
