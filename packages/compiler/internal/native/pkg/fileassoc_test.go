package pkg

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func assocMeta(t *testing.T) Metadata {
	m := testMeta(t)
	m.FileAssociations = []config.NativeFileAssociation{
		{Ext: []string{"md", "Markdown"}, Name: "Markdown document", MIME: "text/markdown", Role: "editor", Icon: writeTestIcon(t, 512, 512)},
		{Ext: []string{"txt"}, Role: "viewer"},
		{Ext: []string{"acme"}},
	}
	return m
}

func wellFormed(t *testing.T, what string, b []byte) {
	t.Helper()
	d := xml.NewDecoder(strings.NewReader(string(b)))
	for {
		_, err := d.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("%s is not well-formed XML: %v\n%s", what, err, b)
		}
	}
}

func TestInfoPlistDocumentTypes(t *testing.T) {
	plist := InfoPlist(assocMeta(t), "Acme Notes")
	wellFormed(t, "Info.plist", plist)
	s := string(plist)
	for _, want := range []string{
		"<key>CFBundleDocumentTypes</key>",
		"<key>CFBundleTypeName</key>\n\t\t\t<string>Markdown document</string>",
		"<key>CFBundleTypeRole</key>\n\t\t\t<string>Editor</string>",
		"<key>LSHandlerRank</key>\n\t\t\t<string>Owner</string>",
		"<string>com.acme.notes.filetype.md</string>",
		"<key>CFBundleTypeExtensions</key>\n\t\t\t<array>\n\t\t\t\t<string>md</string>\n\t\t\t\t<string>markdown</string>",
		"<key>CFBundleTypeMIMETypes</key>\n\t\t\t<array>\n\t\t\t\t<string>text/markdown</string>",
		"<key>CFBundleTypeIconFile</key>\n\t\t\t<string>filetype-md</string>",
		// A system type uses the system UTI and is a viewer here.
		"<string>public.plain-text</string>", "<string>Viewer</string>", "<string>Alternate</string>",
		"<key>UTExportedTypeDeclarations</key>",
		"<key>UTTypeIdentifier</key>\n\t\t\t<string>com.acme.notes.filetype.md</string>",
		"<key>public.filename-extension</key>", "<key>public.mime-type</key>",
		"<string>com.acme.notes.filetype.acme</string>",
		"<string>application/x-acme-notes-acme</string>",
		// Deep links still there.
		"<key>CFBundleURLSchemes</key>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("Info.plist lacks %q\n%s", want, s)
		}
	}
	if strings.Contains(s, "com.acme.notes.filetype.txt") {
		t.Error("a system type must not get an exported UTI")
	}
	if n := strings.Count(s, "<key>UTTypeIdentifier</key>"); n != 2 {
		t.Errorf("exported types = %d, want 2 (md and acme)", n)
	}
	if strings.Contains(string(InfoPlist(testMeta(t), "x")), "CFBundleDocumentTypes") {
		t.Error("an app without associations declares no document types")
	}
}

var _ = zip.Store
