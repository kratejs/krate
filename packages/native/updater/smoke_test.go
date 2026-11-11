package updater

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestSmokeUpdateInstalledApp runs the updater against a real installed app
// and a real signed feed. CI scripts (scripts/native-smoke) install version A
// of a packaged app, serve the feed `krate native package` wrote for version
// B, and run this test with the variables below; the script then checks that
// the installed executable reports version B. Without the variables the test
// is skipped.
//
//	KRATE_UPDATE_SMOKE_FEED     feed URL (http is allowed here)
//	KRATE_UPDATE_SMOKE_KEY      public key printed by krate native keygen
//	KRATE_UPDATE_SMOKE_APP_ID   native.appId
//	KRATE_UPDATE_SMOKE_VERSION  installed version (A)
//	KRATE_UPDATE_SMOKE_EXPECT   version the feed offers (B)
//	KRATE_UPDATE_SMOKE_EXE      installed executable to replace
//	KRATE_UPDATE_SMOKE_APPIMAGE optional AppImage path (Linux AppImage updates)
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
	if last == 0 || u.Path() == "" {
		t.Fatalf("nothing downloaded (%d bytes, path %q)", last, u.Path())
	}
	if err := u.Apply(); err != nil {
		t.Fatal(err)
	}
	CleanupStale(cfg.Exe)
}
