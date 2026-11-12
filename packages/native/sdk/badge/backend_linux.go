//go:build linux

package badge

import (
	"fmt"
	"hash/fnv"
	"os"
	"strconv"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
	"github.com/kratejs/krate/packages/native/sdk/internal/sessionbus"
)

const launcherIface = "com.canonical.Unity.LauncherEntry"

// unityBackend emits LauncherEntry Update signals. The launcher keeps the
// last values per app uri, so every update sends the properties that changed.
type unityBackend struct {
	appURI string
	path   string
	mu     sync.Mutex
}

func newDefaultBackend(env *bridge.Env) (backend, error) {
	if _, err := sessionbus.Get(); err != nil {
		return nil, err
	}
	return &unityBackend{appURI: appURI(), path: entryPath(appURI())}, nil
}

func defaultAvailable() bool {
	_, err := sessionbus.Get()
	return err == nil
}

// appURI is the launcher's name for the app: its desktop file.
func appURI() string {
	id := os.Getenv("KRATE_APP_ID")
	if id == "" {
		id = "krate"
	}
	return "application://" + id + ".desktop"
}

// entryPath derives the object path from the uri the way libunity does: a
// number from a hash of the uri.
func entryPath(uri string) string {
	h := fnv.New32a()
	h.Write([]byte(uri))
	return "/com/canonical/unity/launcherentry/" + strconv.FormatUint(uint64(h.Sum32()), 10)
}

func (b *unityBackend) update(props map[string]dbusiface.Variant) error {
	conn, err := sessionbus.Get()
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return conn.Emit(b.path, launcherIface, "Update", b.appURI, props)
}

// launcherProps builds the Update properties for a badge text, or an error
// when the launcher cannot show it.
func launcherProps(text string) (map[string]dbusiface.Variant, error) {
	if text == "" {
		return map[string]dbusiface.Variant{
			"count":         {Value: int64(0)},
			"count-visible": {Value: false},
		}, nil
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("%w: Linux launchers can only show a number as the badge, not %q", krate.ErrUnsupported, text)
	}
	return map[string]dbusiface.Variant{
		"count":         {Value: n},
		"count-visible": {Value: n > 0},
	}, nil
}

// progressProps builds the Update properties for a progress state.
func progressProps(p caps.Progress) map[string]dbusiface.Variant {
	switch p.State {
	case caps.ProgressNormal, caps.ProgressError, caps.ProgressPaused:
		return map[string]dbusiface.Variant{
			"progress":         {Value: p.Value},
			"progress-visible": {Value: true},
		}
	}
	return map[string]dbusiface.Variant{
		"progress":         {Value: float64(0)},
		"progress-visible": {Value: false},
	}
}

func (b *unityBackend) setBadge(text string) error {
	props, err := launcherProps(text)
	if err != nil {
		return err
	}
	return b.update(props)
}

func (b *unityBackend) setProgress(p caps.Progress) error { return b.update(progressProps(p)) }

func (b *unityBackend) setUrgent(on bool) error {
	return b.update(map[string]dbusiface.Variant{"urgent": {Value: on}})
}
