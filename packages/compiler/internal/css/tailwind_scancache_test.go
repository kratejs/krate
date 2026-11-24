package css

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanClassesKeepsAFilesScanUntilTheFileChanges(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.tsx")
	write := func(src string, mtime time.Time) {
		t.Helper()
		if err := os.WriteFile(file, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	write(`export const A = () => <div className="p-4 text-red-500" style={{ color: "var(--brand)" }} />`, now.Add(-time.Hour))

	scan := func() (map[string]bool, *TailwindScanner) {
		s := NewTailwindScanner(dir)
		return s.ScanClasses([]string{dir}), s
	}
	classes, s := scan()
	if !classes["p-4"] || !classes["text-red-500"] || !s.VarRefs["brand"] {
		t.Fatalf("first scan found %v, refs %v", classes, s.VarRefs)
	}

	// The second scan is the first one's result, without reading the file.
	info, _ := os.Stat(file)
	if _, ok := scannedFiles.get(file, info, namesSignature(nil)); !ok {
		t.Fatal("the scan of an unchanged file was not kept")
	}
	classes, s = scan()
	if !classes["p-4"] || !s.VarRefs["brand"] {
		t.Fatalf("a kept scan lost its classes or variables: %v, refs %v", classes, s.VarRefs)
	}

	// A new size is a new scan, even with the old modification time.
	write(`export const A = () => <div className="m-2 text-blue-500" />`, now.Add(-time.Hour))
	classes, s = scan()
	if classes["p-4"] || !classes["m-2"] || !classes["text-blue-500"] || s.VarRefs["brand"] {
		t.Fatalf("an edited file was taken as unchanged: %v, refs %v", classes, s.VarRefs)
	}

	// A new modification time is a new scan, even with the old size.
	write(`export const A = () => <div className="m-3 text-blue-500" />`, now.Add(-time.Minute))
	classes, _ = scan()
	if classes["m-2"] || !classes["m-3"] {
		t.Fatalf("a touched file was taken as unchanged: %v", classes)
	}

	// The utility names are part of what a scan found.
	s = NewTailwindScanner(dir)
	s.Names = map[string]bool{"card": true}
	write(`export const A = () => <div className="card m-3" />`, now.Add(-time.Second))
	if classes := s.ScanClasses([]string{dir}); !classes["card"] {
		t.Fatalf("a project utility name was not found: %v", classes)
	}
	if classes, _ := scan(); classes["card"] {
		t.Fatalf("a scan made with other utility names was reused: %v", classes)
	}
}
