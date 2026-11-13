package pkg

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// FileType is a file association with its defaults applied.
type FileType struct {
	// ID is a short token unique within the app, made of lowercase letters
	// and digits, for ProgIDs, UTIs and file names.
	ID string
	// Exts are the lowercase extensions without the dot.
	Exts []string
	Name string
	MIME string
	// Editor is false for a viewer.
	Editor bool
	// Icon is the absolute path of the image, "" for the app icon.
	Icon string
}

var nonAlnum = func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') }

// FileTypes returns the app's file associations with defaults applied: the
// name "<EXT> file", the media type application/x-<app>-<extension>, the
// editor role.
func (m Metadata) FileTypes() []FileType {
	var out []FileType
	used := map[string]bool{}
	for _, fa := range m.FileAssociations {
		if len(fa.Ext) == 0 {
			continue
		}
		ft := FileType{Name: oneLine(fa.Name), MIME: fa.MIME, Editor: fa.Role != "viewer", Icon: fa.Icon}
		for _, e := range fa.Ext {
			ft.Exts = append(ft.Exts, strings.ToLower(e))
		}
		first := ft.Exts[0]
		if ft.Name == "" {
			ft.Name = strings.ToUpper(first) + " file"
		}
		if ft.MIME == "" {
			ft.MIME = "application/x-" + m.slug() + "-" + first
		}
		id := strings.Join(strings.FieldsFunc(first, nonAlnum), "")
		if id == "" {
			id = "type"
		}
		base := id
		for i := 2; used[id]; i++ {
			id = fmt.Sprintf("%s%d", base, i)
		}
		used[id] = true
		ft.ID = id
		out = append(out, ft)
	}
	return out
}

// ProgID is the Windows programmatic identifier of the type.
func (ft FileType) ProgID(m Metadata) string { return m.AppID + ".file." + ft.ID }

// iconFile is the name of the type's icon inside the install or Resources
// directory ("" when the type has no icon of its own), without extension.
func (ft FileType) iconFile() string {
	if ft.Icon == "" {
		return ""
	}
	return "filetype-" + ft.ID
}

// systemUTIs maps common extensions to the Uniform Type Identifiers macOS
// itself declares. A type with another extension gets an exported UTI of the
// app's own.
var systemUTIs = map[string]string{
	"txt": "public.plain-text", "text": "public.plain-text", "json": "public.json", "xml": "public.xml",
	"html": "public.html", "htm": "public.html", "css": "public.css", "js": "com.netscape.javascript-source",
	"csv": "public.comma-separated-values-text", "tsv": "public.tab-separated-values-text",
	"rtf": "public.rtf", "pdf": "com.adobe.pdf", "png": "public.png", "jpg": "public.jpeg", "jpeg": "public.jpeg",
	"gif": "com.compuserve.gif", "tif": "public.tiff", "tiff": "public.tiff", "bmp": "com.microsoft.bmp",
	"svg": "public.svg-image", "mp3": "public.mp3", "wav": "com.microsoft.waveform-audio", "m4a": "public.mpeg-4-audio",
	"mp4": "public.mpeg-4", "mov": "com.apple.quicktime-movie", "zip": "public.zip-archive",
	"yaml": "public.yaml", "yml": "public.yaml", "ics": "public.calendar-event", "vcf": "public.vcard",
}

// systemUTI returns the system UTI of the type when every extension maps to
// the same one.
func (ft FileType) systemUTI() string {
	uti := ""
	for _, e := range ft.Exts {
		u, ok := systemUTIs[e]
		if !ok || (uti != "" && u != uti) {
			return ""
		}
		uti = u
	}
	return uti
}

// exportedUTI is the identifier of the UTI the app declares for the type.
func (ft FileType) exportedUTI(m Metadata) string { return m.AppID + ".filetype." + ft.ID }

// plistFileTypes writes CFBundleDocumentTypes and, for the types macOS does
// not know, UTExportedTypeDeclarations.
func plistFileTypes(b *strings.Builder, m Metadata) {
	types := m.FileTypes()
	if len(types) == 0 {
		return
	}
	str := func(indent, k, v string) {
		fmt.Fprintf(b, "%s<key>%s</key>\n%s<string>%s</string>\n", indent, k, indent, xmlEscape(v))
	}
	list := func(indent, k string, vs []string) {
		fmt.Fprintf(b, "%s<key>%s</key>\n%s<array>\n", indent, k, indent)
		for _, v := range vs {
			fmt.Fprintf(b, "%s\t<string>%s</string>\n", indent, xmlEscape(v))
		}
		fmt.Fprintf(b, "%s</array>\n", indent)
	}
	b.WriteString("\t<key>CFBundleDocumentTypes</key>\n\t<array>\n")
	var exported []FileType
	for _, ft := range types {
		sys := ft.systemUTI()
		uti := sys
		if uti == "" {
			uti = ft.exportedUTI(m)
			exported = append(exported, ft)
		}
		role, rank := "Editor", "Owner"
		switch {
		case !ft.Editor:
			role, rank = "Viewer", "Alternate"
		case sys != "":
			rank = "Default"
		}
		b.WriteString("\t\t<dict>\n")
		str("\t\t\t", "CFBundleTypeName", ft.Name)
		str("\t\t\t", "CFBundleTypeRole", role)
		str("\t\t\t", "LSHandlerRank", rank)
		list("\t\t\t", "LSItemContentTypes", []string{uti})
		list("\t\t\t", "CFBundleTypeExtensions", ft.Exts)
		list("\t\t\t", "CFBundleTypeMIMETypes", []string{ft.MIME})
		if f := ft.iconFile(); f != "" {
			str("\t\t\t", "CFBundleTypeIconFile", f)
		}
		b.WriteString("\t\t</dict>\n")
	}
	b.WriteString("\t</array>\n")
	if len(exported) == 0 {
		return
	}
	b.WriteString("\t<key>UTExportedTypeDeclarations</key>\n\t<array>\n")
	for _, ft := range exported {
		b.WriteString("\t\t<dict>\n")
		str("\t\t\t", "UTTypeIdentifier", ft.exportedUTI(m))
		str("\t\t\t", "UTTypeDescription", ft.Name)
		list("\t\t\t", "UTTypeConformsTo", []string{"public.data"})
		if f := ft.iconFile(); f != "" {
			str("\t\t\t", "UTTypeIconFile", f)
		}
		b.WriteString("\t\t\t<key>UTTypeTagSpecification</key>\n\t\t\t<dict>\n")
		list("\t\t\t\t", "public.filename-extension", ft.Exts)
		list("\t\t\t\t", "public.mime-type", []string{ft.MIME})
		b.WriteString("\t\t\t</dict>\n\t\t</dict>\n")
	}
	b.WriteString("\t</array>\n")
}

// SharedMimeXML returns the shared-mime-info package file that teaches the
// desktop the app's file types (usr/share/mime/packages/<app id>.xml), or nil
// when the app has none.
func SharedMimeXML(m Metadata) []byte {
	types := m.FileTypes()
	if len(types) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<mime-info xmlns=\"http://www.freedesktop.org/standards/shared-mime-info\">\n")
	for _, ft := range types {
		fmt.Fprintf(&b, "  <mime-type type=\"%s\">\n", xmlEscape(ft.MIME))
		fmt.Fprintf(&b, "    <comment>%s</comment>\n", xmlEscape(ft.Name))
		for _, e := range ft.Exts {
			fmt.Fprintf(&b, "    <glob pattern=\"*.%s\"/>\n", xmlEscape(e))
			// Extensions are matched case-insensitively by default.
		}
		b.WriteString("  </mime-type>\n")
	}
	b.WriteString("</mime-info>\n")
	return []byte(b.String())
}

// desktopMimeTypes returns the media types for the .desktop MimeType key.
func desktopMimeTypes(m Metadata) []string {
	var out []string
	seen := map[string]bool{}
	for _, ft := range m.FileTypes() {
		if !seen[ft.MIME] {
			seen[ft.MIME] = true
			out = append(out, ft.MIME)
		}
	}
	sort.Strings(out)
	return out
}

// msixFileTypeName returns the Name of a uap:FileTypeAssociation: lowercase
// letters and digits, at most 50 characters.
func msixFileTypeName(ft FileType) string {
	n := ft.ID
	if len(n) > 50 {
		n = n[:50]
	}
	return n
}

// msixLogoPart is the package path of a type's logo.
func msixLogoPart(ft FileType) string { return "Assets/FileType-" + ft.ID + ".png" }

// nsisFileAssoc writes the registry entries that associate the types with the
// app. w writes one script line.
func nsisFileAssoc(w func(string, ...any), m Metadata) {
	for _, ft := range m.FileTypes() {
		prog := ft.ProgID(m)
		w("  WriteRegStr HKCR %s \"\" %s", nsisStr(prog), nsisStr(ft.Name))
		icon := "$INSTDIR\\${APPEXE},0"
		if f := ft.iconFile(); f != "" {
			icon = "$INSTDIR\\" + f + ".ico"
		}
		w("  WriteRegStr HKCR %s \"\" \"%s\"", nsisStr(prog+"\\DefaultIcon"), icon)
		w("  WriteRegStr HKCR %s \"\" '\"$INSTDIR\\${APPEXE}\" \"%%1\"'", nsisStr(prog+"\\shell\\open\\command"))
		for _, e := range ft.Exts {
			ext := "." + e
			// OpenWithProgids puts the app in "Open with" without taking over a
			// type another program owns; an editor claims a type with no handler.
			w("  WriteRegStr HKCR %s %s \"\"", nsisStr(ext+"\\OpenWithProgids"), nsisStr(prog))
			if ft.Editor {
				w("  ReadRegStr $0 HKCR %s \"\"", nsisStr(ext))
				w("  ${If} $0 == \"\"")
				w("    WriteRegStr HKCR %s \"\" %s", nsisStr(ext), nsisStr(prog))
				w("    WriteRegStr HKCR %s \"Content Type\" %s", nsisStr(ext), nsisStr(ft.MIME))
				w("  ${EndIf}")
			}
		}
	}
	if len(m.FileTypes()) > 0 {
		w("  System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, p 0, p 0)'")
	}
}

// nsisFileAssocRemove undoes nsisFileAssoc.
func nsisFileAssocRemove(w func(string, ...any), m Metadata) {
	for _, ft := range m.FileTypes() {
		prog := ft.ProgID(m)
		for _, e := range ft.Exts {
			ext := "." + e
			w("  ReadRegStr $0 HKCR %s \"\"", nsisStr(ext))
			w("  ${If} $0 == %s", nsisStr(prog))
			w("    DeleteRegValue HKCR %s \"\"", nsisStr(ext))
			w("    DeleteRegValue HKCR %s \"Content Type\"", nsisStr(ext))
			w("  ${EndIf}")
			w("  DeleteRegValue HKCR %s %s", nsisStr(ext+"\\OpenWithProgids"), nsisStr(prog))
		}
		w("  DeleteRegKey HKCR %s", nsisStr(prog))
	}
	if len(m.FileTypes()) > 0 {
		w("  System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, p 0, p 0)'")
	}
}

// fileTypeIcon loads the image of a type with its own icon.
func fileTypeIcon(ft FileType) (*Icon, error) {
	ic, err := LoadIcon(ft.Icon, ft.ID)
	if err != nil {
		return nil, fail(diag.CodeNativePkgIcon, ft.Icon, err.Error(),
			"native.fileAssociations[].icon should be a PNG of 512x512 pixels or more")
	}
	return ic, nil
}
