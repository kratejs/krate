package updater

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdkos "github.com/kratejs/krate/packages/native/sdk/os"
)

// smokeStateDir is the state directory the installed app uses
// (<app data dir>/updater), so a rollback armed here is found by its Boot.
func smokeStateDir(t *testing.T, appID string) string {
	t.Helper()
	if err := sdkos.Configure(sdkos.App{ID: appID}); err != nil {
		t.Fatal(err)
	}
	d, err := sdkos.AppDataDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(d, "updater")
}

// TestSmokeUpdateInstalledApp runs the updater against a real installed app
// and a real signed feed. CI scripts (scripts/native-smoke) install version A
// of a packaged app, serve the feed `krate native package` wrote for version
// B, and run this test with the variables below; the script then checks that
// the installed executable reports version B. Without the variables the test
// is skipped.
//
//	KRATE_UPDATE_SMOKE_FEED         feed URL (http is allowed here)
//	KRATE_UPDATE_SMOKE_KEY          public key printed by krate native keygen
//	KRATE_UPDATE_SMOKE_APP_ID       native.appId
//	KRATE_UPDATE_SMOKE_VERSION      installed version (A)
//	KRATE_UPDATE_SMOKE_EXPECT       version the feed offers (B)
//	KRATE_UPDATE_SMOKE_EXE          installed executable to replace
//	KRATE_UPDATE_SMOKE_APPIMAGE     optional AppImage path (Linux AppImage updates)
//	KRATE_UPDATE_SMOKE_DELTA        "1": the update must be a delta update,
//	                                "0": it must be the full file
//	KRATE_UPDATE_SMOKE_ROLLBACK     "1": keep the previous version for rollback
//	                                (the installed app's Boot finds the state)
func TestSmokeUpdateInstalledApp(t *testing.T) {
	feed := os.Getenv("KRATE_UPDATE_SMOKE_FEED")
	if feed == "" {
		t.Skip("KRATE_UPDATE_SMOKE_FEED is not set")
	}
	cfg := Config{
		URL:       feed,
		PublicKey: os.Getenv("KRATE_UPDATE_SMOKE_KEY"),
		AppID:     os.Getenv("KRATE_UPDATE_SMOKE_APP_ID"),
		Version:   os.Getenv("KRATE_UPDATE_SMOKE_VERSION"),
		Exe:       os.Getenv("KRATE_UPDATE_SMOKE_EXE"),
		AppImage:  os.Getenv("KRATE_UPDATE_SMOKE_APPIMAGE"),
		CacheDir:  t.TempDir(),
		Insecure:  true,
		Logf:      t.Logf,
	}
	if os.Getenv("KRATE_UPDATE_SMOKE_ROLLBACK") == "1" {
		// A fresh install: forget versions an earlier run rolled back.
		cfg.StateDir = smokeStateDir(t, cfg.AppID)
		os.RemoveAll(cfg.StateDir)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	u, err := Check(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if u == nil {
		t.Fatal("the feed offers no update newer than " + cfg.Version)
	}
	if want := os.Getenv("KRATE_UPDATE_SMOKE_EXPECT"); u.Version != want {
		t.Fatalf("the feed offers %s, want %s", u.Version, want)
	}
	var last int64
	if err := u.Download(ctx, func(done, total int64) { last = done }); err != nil {
		t.Fatal(err)
	}
	if last == 0 {
		t.Fatalf("nothing downloaded (%d bytes)", last)
	}
	switch os.Getenv("KRATE_UPDATE_SMOKE_DELTA") {
	case "1":
		if !u.Delta() {
			t.Fatalf("expected a delta update, got the full file (delta offered: %v, error: %v)", u.HasDelta(), u.DeltaError())
		}
		t.Logf("delta update: %d bytes downloaded for a %d byte full file", last, u.Size)
	case "0":
		if u.Delta() || u.Path() == "" {
			t.Fatal("expected the full file")
		}
	default:
		if !u.Delta() && u.Path() == "" {
			t.Fatal("nothing staged")
		}
	}
	if err := u.Apply(); err != nil {
		t.Fatal(err)
	}
	CleanupStale(cfg.Exe)
}

// TestSmokeRollbackState checks the state a rolled back install left behind:
// the previous version runs again and the failed one is on record.
//
//	KRATE_UPDATE_SMOKE_APP_ID     native.appId
//	KRATE_UPDATE_SMOKE_ROLLED_BACK_FROM  the version that failed
//	KRATE_UPDATE_SMOKE_VERSION    the version that was restored
func TestSmokeRollbackState(t *testing.T) {
	failed := os.Getenv("KRATE_UPDATE_SMOKE_ROLLED_BACK_FROM")
	if failed == "" {
		t.Skip("KRATE_UPDATE_SMOKE_ROLLED_BACK_FROM is not set")
	}
	st := LoadState(smokeStateDir(t, os.Getenv("KRATE_UPDATE_SMOKE_APP_ID")))
	if st.Phase != PhaseRolledBack || st.Event == nil {
		t.Fatalf("no rollback on record: %+v", st)
	}
	if st.Event.Failed != failed || st.Event.Restored != os.Getenv("KRATE_UPDATE_SMOKE_VERSION") || st.Event.Reason == "" {
		t.Fatalf("event %+v", st.Event)
	}
	t.Logf("rollback event: %+v", st.Event)
}
