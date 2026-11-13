package pkg

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Resource types.
const (
	rtIcon      = 3
	rtGroupIcon = 14
	rtVersion   = 16
	rtManifest  = 24
	langEnUS    = 0x0409
)

// WindowsResources writes a COFF object (.syso) holding the Windows
// resources of the app: the icon (RT_GROUP_ICON and RT_ICON), the version
// information (RT_VERSION) and an application manifest that enables
// per-monitor DPI awareness and Common Controls v6.
//
// The Go linker links a .syso found in the main package directory. Name it for
// its target so other platforms ignore it, and put it next to main.go:
//
//	rsrc_windows_amd64.syso
//	rsrc_windows_arm64.syso
//
// The architecture is taken from that suffix (amd64 when absent). root is
// the project directory that native.icons resolves against.
func WindowsResources(root string, cfg *config.Config, outSyso string) error {
	m := MetadataFromConfig(root, cfg)
	arch := "amd64"
	base := strings.TrimSuffix(filepath.Base(outSyso), ".syso")
	for _, a := range []string{"amd64", "arm64", "386"} {
		if strings.HasSuffix(base, "_windows_"+a) {
			arch = a
		}
	}
	ic, err := LoadIcon(m.Icon, m.AppID)
	if err != nil {
		return fail(diag.CodeNativePkgIcon, m.Icon, err.Error(), "native.icons should be a PNG of 1024x1024 pixels")
	}
	data, err := buildSyso(m, ic, arch)
	if err != nil {
		return err
	}
	return wrapWrite(outSyso, writeFileAtomic(outSyso, data))
}

type resLeaf struct {
	typ, name, lang uint32
	data            []byte
}

func buildSyso(m Metadata, ic *Icon, arch string) ([]byte, error) {
	v, ok := splitVersion(m.Version)
	if !ok {
		return nil, fail(diag.CodeNativePkgMeta, "native.version", fmt.Sprintf("version %q is not MAJOR.MINOR.PATCH", m.Version), "")
	}
	sizes, imgs, err := ic.icoImages()
	if err != nil {
		return nil, err
	}
	var leaves []resLeaf
	var grp bytes.Buffer
	putBin(&grp, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	for i, s := range sizes {
		id := uint32(i + 1)
		leaves = append(leaves, resLeaf{rtIcon, id, langEnUS, imgs[i]})
		dim := byte(s)
		if s >= 256 {
			dim = 0
		}
		grp.Write([]byte{dim, dim, 0, 0})
		putBin(&grp, binary.LittleEndian, uint16(1))
		putBin(&grp, binary.LittleEndian, uint16(32))
		putBin(&grp, binary.LittleEndian, uint32(len(imgs[i])))
		putBin(&grp, binary.LittleEndian, uint16(id))
	}
	leaves = append(leaves,
		resLeaf{rtGroupIcon, 1, langEnUS, grp.Bytes()},
		resLeaf{rtVersion, 1, langEnUS, versionInfo(m, v)},
		resLeaf{rtManifest, 1, langEnUS, []byte(windowsManifest(m, v))},
	)
	rsrc, relocs := buildResourceTree(leaves)
	return coffObject(arch, rsrc, relocs)
}

// buildResourceTree lays out the type/name/language directory tree and the
// data entries. It returns the section bytes and the offsets of every
// OffsetToData field, which need an RVA relocation.
func buildResourceTree(leaves []resLeaf) ([]byte, []uint32) {
	sort.SliceStable(leaves, func(i, j int) bool {
		a, b := leaves[i], leaves[j]
		if a.typ != b.typ {
			return a.typ < b.typ
		}
		if a.name != b.name {
			return a.name < b.name
		}
		return a.lang < b.lang
	})
	type nameNode struct {
		id    uint32
		langs []int
	}
	type typeNode struct {
		id    uint32
		names []*nameNode
	}
	var types []*typeNode
	for i, l := range leaves {
		if len(types) == 0 || types[len(types)-1].id != l.typ {
			types = append(types, &typeNode{id: l.typ})
		}
		t := types[len(types)-1]
		if len(t.names) == 0 || t.names[len(t.names)-1].id != l.name {
			t.names = append(t.names, &nameNode{id: l.name})
		}
		n := t.names[len(t.names)-1]
		n.langs = append(n.langs, i)
	}
	// Sizes: root, type dirs, name dirs, data entries, then data.
	off := uint32(16 + 8*len(types))
	typeOff := make([]uint32, len(types))
	nameOff := map[*nameNode]uint32{}
	for i, t := range types {
		typeOff[i] = off
		off += uint32(16 + 8*len(t.names))
	}
	for _, t := range types {
		for _, n := range t.names {
			nameOff[n] = off
			off += uint32(16 + 8*len(n.langs))
		}
	}
	entryOff := make([]uint32, len(leaves))
	for i := range leaves {
		entryOff[i] = off
		off += 16
	}
	dataOff := make([]uint32, len(leaves))
	for i, l := range leaves {
		dataOff[i] = off
		off += uint32(len(l.data))
		off = (off + 3) &^ 3
	}
	buf := make([]byte, off)
	le := binary.LittleEndian
	dir := func(at uint32, entries int) {
		le.PutUint16(buf[at+14:], uint16(entries)) // NumberOfIdEntries
	}
	dir(0, len(types))
	for i, t := range types {
		le.PutUint32(buf[16+8*i:], t.id)
		le.PutUint32(buf[16+8*i+4:], 0x80000000|typeOff[i])
		dir(typeOff[i], len(t.names))
		for j, n := range t.names {
			e := typeOff[i] + 16 + uint32(8*j)
			le.PutUint32(buf[e:], n.id)
			le.PutUint32(buf[e+4:], 0x80000000|nameOff[n])
			dir(nameOff[n], len(n.langs))
			for k, li := range n.langs {
				e := nameOff[n] + 16 + uint32(8*k)
				le.PutUint32(buf[e:], leaves[li].lang)
				le.PutUint32(buf[e+4:], entryOff[li])
			}
		}
	}
	var relocs []uint32
	for i, l := range leaves {
		e := entryOff[i]
		le.PutUint32(buf[e:], dataOff[i]) // an RVA after relocation
		le.PutUint32(buf[e+4:], uint32(len(l.data)))
		le.PutUint32(buf[e+8:], 0) // code page
		relocs = append(relocs, e)
		copy(buf[dataOff[i]:], l.data)
	}
	return buf, relocs
}

// coffObject wraps a .rsrc section in a COFF object file.
func coffObject(arch string, rsrc []byte, relocs []uint32) ([]byte, error) {
	var machine, relType uint16
	switch arch {
	case "amd64":
		machine, relType = 0x8664, 0x0003 // IMAGE_REL_AMD64_ADDR32NB
	case "arm64":
		machine, relType = 0xAA64, 0x0002 // IMAGE_REL_ARM64_ADDR32NB
	case "386":
		machine, relType = 0x014c, 0x0007 // IMAGE_REL_I386_DIR32NB
	default:
		return nil, fmt.Errorf("unsupported architecture %q", arch)
	}
	const headerLen, sectionLen = 20, 40
	rawOff := uint32(headerLen + sectionLen)
	relocOff := rawOff + uint32(len(rsrc))
	symOff := relocOff + uint32(10*len(relocs))
	var b bytes.Buffer
	w := func(v any) { putBin(&b, binary.LittleEndian, v) }
	// File header.
	w(machine)
	w(uint16(1)) // sections
	w(uint32(0)) // timestamp
	w(symOff)
	w(uint32(1)) // symbols
	w(uint16(0)) // optional header
	w(uint16(0)) // characteristics
	// Section header.
	b.WriteString(".rsrc\x00\x00\x00")
	w(uint32(0)) // virtual size
	w(uint32(0)) // virtual address
	w(uint32(len(rsrc)))
	w(rawOff)
	w(relocOff)
	w(uint32(0)) // line numbers
	w(uint16(len(relocs)))
	w(uint16(0))
	w(uint32(0x40000040)) // initialized data, readable
	b.Write(rsrc)
	for _, r := range relocs {
		w(r)
		w(uint32(0)) // symbol 0
		w(relType)
	}
	// The one symbol: the section itself.
	b.WriteString(".rsrc\x00\x00\x00")
	w(uint32(0))
	w(int16(1))
	w(uint16(0))
	w(uint8(3)) // static
	w(uint8(0))
	w(uint32(4)) // empty string table
	return b.Bytes(), nil
}

func utf16z(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*(len(u)+1))
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	return b
}

func pad4(b *bytes.Buffer) {
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
}

// verBlock builds one VS_VERSIONINFO block. valLen is wValueLength.
func verBlock(key string, wType uint16, valLen uint16, value []byte, children []byte) []byte {
	var b bytes.Buffer
	b.Write([]byte{0, 0}) // wLength, patched below
	putBin(&b, binary.LittleEndian, valLen)
	putBin(&b, binary.LittleEndian, wType)
	b.Write(utf16z(key))
	pad4(&b)
	b.Write(value)
	if len(children) > 0 {
		pad4(&b)
		b.Write(children)
	}
	out := b.Bytes()
	binary.LittleEndian.PutUint16(out, uint16(len(out)))
	return out
}

func joinBlocks(blocks ...[]byte) []byte {
	var b bytes.Buffer
	for i, blk := range blocks {
		if i > 0 {
			pad4(&b)
		}
		b.Write(blk)
	}
	return b.Bytes()
}

func versionInfo(m Metadata, v semver) []byte {
	fv := v.fileVersion()
	var fixed bytes.Buffer
	w := func(x uint32) { putBin(&fixed, binary.LittleEndian, x) }
	ms := uint32(fv[0])<<16 | uint32(fv[1])
	ls := uint32(fv[2])<<16 | uint32(fv[3])
	w(0xFEEF04BD)
	w(0x00010000)
	w(ms)
	w(ls)
	w(ms)
	w(ls)
	w(0x3f)
	w(0)
	w(0x00040004) // VOS_NT_WINDOWS32
	w(1)          // VFT_APP
	w(0)
	w(0)
	w(0)
	publisher := m.Publisher
	if publisher == "" {
		publisher = m.ProductName
	} else {
		publisher = publisherDisplay(publisher)
	}
	strs := [][2]string{
		{"CompanyName", publisher},
		{"FileDescription", m.ProductName},
		{"FileVersion", v.core() + ".0"},
		{"InternalName", m.slug()},
		{"OriginalFilename", winExeName(m)},
		{"ProductName", m.ProductName},
		{"ProductVersion", m.Version},
	}
	var entries [][]byte
	for _, kv := range strs {
		val := utf16z(kv[1])
		entries = append(entries, verBlock(kv[0], 1, uint16(len(val)/2), val, nil))
	}
	table := verBlock("040904B0", 1, 0, nil, joinBlocks(entries...))
	sfi := verBlock("StringFileInfo", 1, 0, nil, table)
	trans := []byte{0x09, 0x04, 0xB0, 0x04}
	vfi := verBlock("VarFileInfo", 1, 0, nil, verBlock("Translation", 0, 4, trans, nil))
	return verBlock("VS_VERSION_INFO", 0, 52, fixed.Bytes(), joinBlocks(sfi, vfi))
}

func windowsManifest(m Metadata, v semver) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0" xmlns:asmv3="urn:schemas-microsoft-com:asm.v3">
  <assemblyIdentity type="win32" name="` + xmlEscape(m.AppID) + `" version="` + v.core() + `.0" processorArchitecture="*"/>
  <description>` + xmlEscape(m.ProductName) + `</description>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
  <asmv3:application>
    <asmv3:windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2,PerMonitor</dpiAwareness>
      <longPathAware xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">true</longPathAware>
    </asmv3:windowsSettings>
  </asmv3:application>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
    </application>
  </compatibility>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
</assembly>
`
}
