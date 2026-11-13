package pkg

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

func TestFileTypesDefaults(t *testing.T) {
	got := assocMeta(t).FileTypes()
	if len(got) != 3 {
		t.Fatalf("types = %+v", got)
	}
	md, txt := got[0], got[1]
	if strings.Join(md.Exts, ",") != "md,markdown" || md.ID != "md" || md.Name != "Markdown document" || md.MIME != "text/markdown" || !md.Editor {
		t.Errorf("md = %+v", md)
	}
	if txt.Name != "TXT file" || txt.MIME != "application/x-acme-notes-txt" || txt.Editor {
		t.Errorf("txt = %+v", txt)
	}
	if md.ProgID(assocMeta(t)) != "com.acme.notes.file.md" {
		t.Errorf("progid = %s", md.ProgID(assocMeta(t)))
	}
}

func TestFileTypeIDsAreUnique(t *testing.T) {
	m := testMeta(t)
	m.FileAssociations = []config.NativeFileAssociation{{Ext: []string{"c++"}}, {Ext: []string{"c"}}, {Ext: []string{"c-"}}}
	var ids []string
	for _, ft := range m.FileTypes() {
		ids = append(ids, ft.ID)
	}
	if strings.Join(ids, ",") != "c,c2,c3" {
		t.Errorf("ids = %v", ids)
	}
}

func TestMetadataFromConfigResolvesAssociationIcons(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Native = &config.NativeConfig{AppID: "com.acme.notes", FileAssociations: []config.NativeFileAssociation{
		{Ext: []string{"md"}, Icon: "assets/md.png"}, {Ext: []string{"txt"}, Icon: filepath.Join(root, "abs.png")}, {Ext: []string{"log"}},
	}}
	m := MetadataFromConfig(root, cfg)
	if m.FileAssociations[0].Icon != filepath.Join(root, "assets/md.png") || m.FileAssociations[1].Icon != filepath.Join(root, "abs.png") || m.FileAssociations[2].Icon != "" {
		t.Errorf("icons = %+v", m.FileAssociations)
	}
	if cfg.Native.FileAssociations[0].Icon != "assets/md.png" {
		t.Error("the config was modified")
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

func TestMacBundleHasTypeIcons(t *testing.T) {
	res := run(t, Options{Meta: assocMeta(t), Payload: testPayload(t, "darwin", "arm64")})
	app := find(t, res, "app").Path
	b, err := os.ReadFile(filepath.Join(app, "Contents/Resources/filetype-md.icns"))
	if err != nil || len(b) < 100 || string(b[:4]) != "icns" {
		t.Fatalf("filetype-md.icns: %v (%d bytes)", err, len(b))
	}
	if _, err := os.Stat(filepath.Join(app, "Contents/Resources/filetype-txt.icns")); err == nil {
		t.Error("a type without an icon of its own must not get one")
	}
}

func TestMissingTypeIconIsADiagnostic(t *testing.T) {
	m := assocMeta(t)
	m.FileAssociations[0].Icon = filepath.Join(t.TempDir(), "gone.png")
	_, err := Package(Options{Meta: m, Payload: testPayload(t, "darwin", "arm64"), OutDir: t.TempDir(), Runner: &fakeRunner{goos: "linux"}})
	pe, ok := err.(*Error)
	if !ok || !strings.Contains(pe.Diag.Hint, "fileAssociations") {
		t.Fatalf("err = %v", err)
	}
}

func TestAppxManifestFileTypes(t *testing.T) {
	m := assocMeta(t)
	mf := AppxManifest(m, Payload{Arch: "amd64"})
	wellFormed(t, "AppxManifest.xml", mf)
	var doc struct {
		Exts []struct {
			Category string `xml:"Category,attr"`
			FTA      struct {
				Name        string `xml:"Name,attr"`
				DisplayName string `xml:"DisplayName"`
				Logo        string `xml:"Logo"`
				Types       []struct {
					CT  string `xml:"ContentType,attr"`
					Ext string `xml:",chardata"`
				} `xml:"SupportedFileTypes>FileType"`
			} `xml:"FileTypeAssociation"`
		} `xml:"Applications>Application>Extensions>Extension"`
	}
	if err := xml.Unmarshal(mf, &doc); err != nil {
		t.Fatal(err)
	}
	var fta int
	for _, e := range doc.Exts {
		if e.Category != "windows.fileTypeAssociation" {
			continue
		}
		fta++
		if e.FTA.Name == "md" {
			if e.FTA.DisplayName != "Markdown document" || len(e.FTA.Types) != 2 || e.FTA.Types[0].Ext != ".md" || e.FTA.Types[1].Ext != ".markdown" ||
				e.FTA.Logo != `Assets\FileType-md.png` || e.FTA.Types[0].CT != "text/markdown" {
				t.Errorf("md = %+v", e.FTA)
			}
		}
	}
	if fta != 3 {
		t.Errorf("fileTypeAssociation extensions = %d, want 3", fta)
	}
	if !strings.Contains(string(mf), "windows.protocol") {
		t.Error("deep links lost")
	}
}

func TestMSIXShipsTypeLogos(t *testing.T) {
	m := assocMeta(t)
	m.Installer = "msix"
	res := run(t, Options{Meta: m, Payload: testPayload(t, "windows", "amd64")})
	es := zipEntries(t, find(t, res, "msix").Path)
	if es["Assets/FileType-md.png"] == nil {
		t.Errorf("no file type logo in the package (have %v)", keys(es))
	}
	if es["Assets/FileType-txt.png"] != nil {
		t.Error("unexpected logo for a type without an icon")
	}
}

func TestNSISFileAssociations(t *testing.T) {
	s := NSISScript(assocMeta(t), Payload{}, `C:\stage\Acme Notes`, `C:\out\setup.exe`)
	for _, want := range []string{
		`WriteRegStr HKCR "com.acme.notes.file.md" "" "Markdown document"`,
		`WriteRegStr HKCR "com.acme.notes.file.md\DefaultIcon" "" "$INSTDIR\filetype-md.ico"`,
		`WriteRegStr HKCR "com.acme.notes.file.md\shell\open\command" "" '"$INSTDIR\${APPEXE}" "%1"'`,
		`WriteRegStr HKCR ".md\OpenWithProgids" "com.acme.notes.file.md" ""`,
		`WriteRegStr HKCR ".markdown\OpenWithProgids" "com.acme.notes.file.md" ""`,
		`ReadRegStr $0 HKCR ".md" ""`,
		`WriteRegStr HKCR ".md" "Content Type" "text/markdown"`,
		`WriteRegStr HKCR "com.acme.notes.file.txt\DefaultIcon" "" "$INSTDIR\${APPEXE},0"`,
		`SHChangeNotify`,
		// Uninstall.
		`DeleteRegKey HKCR "com.acme.notes.file.md"`,
		`DeleteRegValue HKCR ".md\OpenWithProgids" "com.acme.notes.file.md"`,
		`${If} $0 == "com.acme.notes.file.md"`,
		// Deep links still there.
		`DeleteRegKey HKCR "acme"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %q", want)
		}
	}
	// A viewer never takes over a type with no handler.
	installPart, _, _ := strings.Cut(s, `Section "Uninstall"`)
	if strings.Contains(installPart, `ReadRegStr $0 HKCR ".txt" ""`) || strings.Contains(installPart, `WriteRegStr HKCR ".txt" ""`) {
		t.Error("a viewer set itself as the default handler")
	}
	// The Uninstall section removes what the Install section wrote.
	inst, uninst, _ := strings.Cut(s, `Section "Uninstall"`)
	for _, ext := range []string{".md", ".markdown", ".txt", ".acme"} {
		if !strings.Contains(inst, `"`+ext+`\OpenWithProgids"`) || !strings.Contains(uninst, `"`+ext+`\OpenWithProgids"`) {
			t.Errorf("%s is not written and removed", ext)
		}
	}
	if strings.Contains(NSISScript(testMeta(t), Payload{}, "x", "y"), "OpenWithProgids") {
		t.Error("registry entries without associations")
	}
}

func TestNSISInstallTreeHasTypeIcons(t *testing.T) {
	m := assocMeta(t)
	res := run(t, Options{Meta: m, Payload: testPayload(t, "windows", "amd64"), Formats: []string{"zip"}})
	es := zipEntries(t, find(t, res, "zip").Path)
	if es["Acme Notes/filetype-md.ico"] == nil {
		t.Errorf("no type icon next to the executable (have %v)", keys(es))
	}
}

func TestSharedMimeXML(t *testing.T) {
	x := SharedMimeXML(assocMeta(t))
	wellFormed(t, "shared-mime-info", x)
	var doc struct {
		Types []struct {
			Type    string `xml:"type,attr"`
			Comment string `xml:"comment"`
			G       []struct {
				P string `xml:"pattern,attr"`
			} `xml:"glob"`
		} `xml:"mime-type"`
	}
	if err := xml.Unmarshal(x, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Types) != 3 || doc.Types[0].Type != "text/markdown" || doc.Types[0].Comment != "Markdown document" ||
		len(doc.Types[0].G) != 2 || doc.Types[0].G[0].P != "*.md" || doc.Types[0].G[1].P != "*.markdown" ||
		doc.Types[2].Type != "application/x-acme-notes-acme" {
		t.Errorf("types = %+v\n%s", doc.Types, x)
	}
	if !strings.Contains(string(x), `xmlns="http://www.freedesktop.org/standards/shared-mime-info"`) {
		t.Error("no namespace")
	}
	if SharedMimeXML(testMeta(t)) != nil {
		t.Error("an app without associations ships no mime file")
	}
}

func TestDesktopFileMimeTypes(t *testing.T) {
	d := string(desktopFile(assocMeta(t), "/usr/bin/acme-notes"))
	for _, want := range []string{
		"Exec=/usr/bin/acme-notes %U", // schemes and files together
		"MimeType=x-scheme-handler/acme;x-scheme-handler/acme-dev;application/x-acme-notes-acme;application/x-acme-notes-txt;text/markdown;",
	} {
		if !strings.Contains(d, want) {
			t.Errorf(".desktop lacks %q:\n%s", want, d)
		}
	}
	m := assocMeta(t)
	m.DeepLinks = nil
	d = string(desktopFile(m, "/usr/bin/acme-notes"))
	if !strings.Contains(d, "Exec=/usr/bin/acme-notes %F\n") || strings.Contains(d, "x-scheme-handler") {
		t.Errorf("files only:\n%s", d)
	}
}

func TestLinuxPackagesCarryTheMimeFile(t *testing.T) {
	m := assocMeta(t)
	res := run(t, Options{Meta: m, Payload: testPayload(t, "linux", "amd64"), Formats: []string{"deb", "rpm"}})
	const mimePath = "usr/share/mime/packages/com.acme.notes.xml"
	if _, err := exec.LookPath("dpkg-deb"); err == nil {
		deb := find(t, res, "deb").Path
		dest := t.TempDir()
		if out, err := exec.Command("dpkg-deb", "-x", deb, dest).CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		b, err := os.ReadFile(filepath.Join(dest, mimePath))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != string(SharedMimeXML(m)) {
			t.Errorf("deb mime file differs:\n%s", b)
		}
		d, _ := os.ReadFile(filepath.Join(dest, "usr/share/applications/com.acme.notes.desktop"))
		if !strings.Contains(string(d), "text/markdown;") {
			t.Errorf("deb desktop file:\n%s", d)
		}
	} else {
		t.Log("dpkg-deb not installed; the deb is not unpacked")
	}
	rpm, _ := os.ReadFile(find(t, res, "rpm").Path)
	if !strings.Contains(string(rpm), "com.acme.notes.xml") || !strings.Contains(string(rpm), "/usr/share/mime/packages/") {
		t.Error("rpm lacks the mime file")
	}
}

func TestAppImageCarriesTheMimeFile(t *testing.T) {
	fr := &fakeRunner{tools: map[string]bool{"appimagetool": true}}
	res := run(t, Options{Meta: assocMeta(t), Payload: testPayload(t, "linux", "amd64"), Formats: []string{"appimage"}, Runner: fr})
	dir := filepath.Join(res.Stage, "acme-notes.AppDir")
	b, err := os.ReadFile(filepath.Join(dir, "usr/share/mime/packages/com.acme.notes.xml"))
	if err != nil || !strings.Contains(string(b), "text/markdown") {
		t.Fatalf("AppDir mime file: %v %s", err, b)
	}
	d, _ := os.ReadFile(filepath.Join(dir, "acme-notes.desktop"))
	if !strings.Contains(string(d), "MimeType=") || !strings.Contains(string(d), "text/markdown;") {
		t.Errorf("AppDir desktop file:\n%s", d)
	}
}

func TestNoAssociationsChangeNothing(t *testing.T) {
	// The zip package of an app without associations has no type icons.
	res := run(t, Options{Meta: testMeta(t), Payload: testPayload(t, "windows", "amd64"), Formats: []string{"zip"}})
	for name := range zipEntries(t, find(t, res, "zip").Path) {
		if strings.Contains(name, "filetype-") {
			t.Errorf("unexpected %s", name)
		}
	}
}

var _ = zip.Store
