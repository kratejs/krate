package sdkupdater

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/distinfo"
)

func TestUpdaterRefusesInsideAStore(t *testing.T) {
	Configure(Config{URL: "https://example.com/{target}.json", PublicKey: "k"})
	t.Cleanup(func() { Configure(Config{}) })
	for _, store := range []distinfo.Store{distinfo.Flatpak, distinfo.Snap, distinfo.MAS, distinfo.MSStore} {
		restore := distinfo.Override(distinfo.Info{Store: store})
		_, err := current()
		if err == nil || !strings.Contains(err.Error(), "updates are not available here") {
			t.Errorf("%s: err = %v", store, err)
		}
		restore()
	}
}

func TestUniversalTargetReachesTheFeedConfig(t *testing.T) {
	Configure(Config{URL: "https://example.com/{target}.json", PublicKey: "k"})
	t.Cleanup(func() { Configure(Config{}) })
	defer distinfo.Override(distinfo.Info{Target: "darwin-universal"})()
	c, err := current()
	if err != nil {
		t.Skipf("no app identity in this test process: %v", err)
	}
	if c.Target != "darwin-universal" {
		t.Errorf("Target = %q", c.Target)
	}
}
