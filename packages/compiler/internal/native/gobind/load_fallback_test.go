package gobind

import (
	"go/token"
	"go/types"
	"path/filepath"
	"testing"
)

type panicImporter struct{}

func (panicImporter) Import(path string) (*types.Package, error) {
	panic("cannot decode \"" + path + "\", export data version 5 is greater than maximum supported version 2")
}

func listFixture(t *testing.T) (*listedPkg, map[string]*listedPkg) {
	t.Helper()
	root := newApp(t, "app")
	dir := filepath.Join(root, "src", "lib")
	listed, byDir, goErr, err := goList(root, []string{"./src/lib"}, nil)
	if err != nil || goErr != "" {
		t.Fatalf("goList: %v %s", err, goErr)
	}
	lp := listed[byDir[filepath.Clean(dir)]]
	if lp == nil {
		t.Fatal("fixture package not listed")
	}
	return lp, listed
}

func TestSafeCheckPackageRecoversExportDataPanic(t *testing.T) {
	lp, _ := listFixture(t)
	if _, _, ok := safeCheckPackage(token.NewFileSet(), panicImporter{}, lp); ok {
		t.Fatal("an unreadable export data panic must report ok=false")
	}
}

func TestSourceImporterFallbackChecksPackage(t *testing.T) {
	lp, listed := listFixture(t)
	fset := token.NewFileSet()
	exp := newExportImporter(fset, listed)
	exp.useSource(fset)
	up, errs, ok := safeCheckPackage(fset, exp.forPackage(lp), lp)
	if !ok || len(errs) > 0 || up == nil {
		t.Fatalf("source-checked package failed: ok=%v errs=%v", ok, errs)
	}
}
