//go:build linux

package linux

import (
	"os"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/internal/memory"
)

// Features says which optional engine features the app uses. The memory
// profiles other than "performance" switch the unused ones off.
type Features struct {
	Media, WebAudio, WebGL, WebRTC, SpellCheck bool
}

// Memory is the webview memory profile (see package internal/memory).
type Memory struct {
	// Profile is "balanced", "minimal" or "performance". Empty means
	// "performance": none of the levers below run.
	Profile  string
	Features Features
}

func (m Memory) profile() memory.Profile {
	if m.Profile == "" {
		return memory.Performance
	}
	return memory.Parse(m.Profile)
}

// pressure holds the WebKit memory pressure handler settings of a profile.
// The limit is in MiB and applies to each WebKit child process; the
// thresholds are fractions of the limit; poll is in seconds.
type pressure struct {
	limit        uint32
	conservative float64
	strict       float64
	poll         float64
}

// pressureFor returns the settings of a profile, ok false for performance.
func pressureFor(p memory.Profile) (pressure, bool) {
	switch p {
	case memory.Balanced:
		return pressure{limit: 200, conservative: 0.5, strict: 0.75, poll: 5}, true
	case memory.Minimal:
		return pressure{limit: 150, conservative: 0.4, strict: 0.6, poll: 3}, true
	}
	return pressure{}, false
}

// WebKitCacheModel values.
const cacheModelDocumentViewer = 0

// memAPI is the optional part of the WebKit API the levers use. Every field
// may be nil (older WebKitGTK); each lever skips what it lacks.
type memAPI struct {
	pressureNew          func() uintptr
	pressureFree         func(s uintptr)
	pressureLimit        func(s uintptr, mb uint32)
	pressureConservative func(s uintptr, f float64)
	pressureStrict       func(s uintptr, f float64)
	pressureKill         func(s uintptr, f float64)
	pressurePoll         func(s uintptr, sec float64)
	setPressure          func(s uintptr)

	dataMgrNew3     func(k1, v1, k2, v2, k3, v3 string, end uintptr) uintptr
	setCacheModel   func(ctx uintptr, model int32)
	setFavicons     func(mgr uintptr, on int32)
	setITP          func(mgr uintptr, on int32)
	setSpellChecked func(ctx uintptr, on int32)

	// settings maps a WebKitSettings property to its setter. A property the
	// loaded WebKitGTK lacks is absent.
	settings map[string]func(s uintptr, on int32)
}

// settingSymbols are the boolean WebKitSettings the profiles drive, by
// property name. The setter symbol is checked with dlsym at load time, so a
// property removed or never added in this WebKitGTK is skipped.
var settingSymbols = map[string]string{
	"enable-media":                         "webkit_settings_set_enable_media",
	"enable-mediasource":                   "webkit_settings_set_enable_mediasource",
	"enable-media-stream":                  "webkit_settings_set_enable_media_stream",
	"enable-webaudio":                      "webkit_settings_set_enable_webaudio",
	"enable-webgl":                         "webkit_settings_set_enable_webgl",
	"enable-webrtc":                        "webkit_settings_set_enable_webrtc",
	"enable-encrypted-media":               "webkit_settings_set_enable_encrypted_media",
	"enable-dns-prefetching":               "webkit_settings_set_enable_dns_prefetching",
	"enable-offline-web-application-cache": "webkit_settings_set_enable_offline_web_application_cache",
	"enable-page-cache":                    "webkit_settings_set_enable_page_cache",
}

func loadMemAPI() *memAPI {
	m := &memAPI{settings: map[string]func(uintptr, int32){}}
	wk, err := purego.Dlopen("libwebkit2gtk-4.1.so.0", rtldNow|rtldGlobal)
	if err != nil {
		return m
	}
	b := &binder{}
	b.bind(&m.pressureNew, wk, "webkit_memory_pressure_settings_new")
	b.bind(&m.pressureFree, wk, "webkit_memory_pressure_settings_free")
	b.bind(&m.pressureLimit, wk, "webkit_memory_pressure_settings_set_memory_limit")
	b.bind(&m.pressureConservative, wk, "webkit_memory_pressure_settings_set_conservative_threshold")
	b.bind(&m.pressureStrict, wk, "webkit_memory_pressure_settings_set_strict_threshold")
	b.bind(&m.pressureKill, wk, "webkit_memory_pressure_settings_set_kill_threshold")
	b.bind(&m.pressurePoll, wk, "webkit_memory_pressure_settings_set_poll_interval")
	b.bind(&m.setPressure, wk, "webkit_website_data_manager_set_memory_pressure_settings")
	b.bind(&m.dataMgrNew3, wk, "webkit_website_data_manager_new")
	b.bind(&m.setCacheModel, wk, "webkit_web_context_set_cache_model")
	b.bind(&m.setFavicons, wk, "webkit_website_data_manager_set_favicons_enabled")
	b.bind(&m.setITP, wk, "webkit_website_data_manager_set_itp_enabled")
	b.bind(&m.setSpellChecked, wk, "webkit_web_context_set_spell_checking_enabled")
	for prop, sym := range settingSymbols {
		var fn func(uintptr, int32)
		if _, err := purego.Dlsym(wk, sym); err != nil {
			continue
		}
		purego.RegisterLibFunc(&fn, wk, sym)
		m.settings[prop] = fn
	}
	return m
}

// applyMemoryPressure installs the memory pressure handler settings. They
// are global to the process and WebKit reads them when it starts a child
// process, so this must run before the first context is created. Saves: the
// web and network processes release caches and (strict threshold) run a
// full GC and flush the page and font caches when they pass the limit,
// instead of keeping them until the system runs short of memory.
func (m *memAPI) applyMemoryPressure(p memory.Profile) {
	pr, ok := pressureFor(p)
	if !ok || m.pressureNew == nil || m.setPressure == nil || m.pressureLimit == nil {
		return
	}
	s := m.pressureNew()
	if s == 0 {
		return
	}
	m.pressureLimit(s, pr.limit)
	// WebKit requires conservative < strict, and checks each call against
	// the current values (defaults 0.33 and 0.5), so order matters.
	if m.pressureStrict != nil {
		m.pressureStrict(s, pr.strict)
	}
	if m.pressureConservative != nil {
		m.pressureConservative(s, pr.conservative)
	}
	if m.pressureKill != nil {
		m.pressureKill(s, 0) // never kill the process
	}
	if m.pressurePoll != nil {
		m.pressurePoll(s, pr.poll)
	}
	m.setPressure(s)
	if m.pressureFree != nil {
		m.pressureFree(s)
	}
}

// applyJSCOptions trims the JavaScript engine of the WebKit child processes.
// Only the minimal profile does: it turns the JIT tiers off in the web and
// network processes, which saves a few MB of private memory and the JIT
// code region per process and costs CPU on heavy page scripts (interpreted
// JavaScript runs several times slower than JIT-compiled code). The child
// processes inherit the environment, so this runs before the first starts.
// A value the user set wins.
func applyJSCOptions(p memory.Profile) {
	if p == memory.Minimal && os.Getenv("JSC_useJIT") == "" {
		_ = os.Setenv("JSC_useJIT", "false")
	}
}

// newDataManager creates the website data manager. Persistent storage
// (localStorage, IndexedDB, cookies) stays under the app's data directory.
// Outside the performance profile the HTTP disk cache moves to a throwaway
// directory: the app's assets are served from the custom scheme and never
// cached on disk, so the network process has nothing useful to keep. The
// returned function removes that directory.
func (m *memAPI) newDataManager(a *api, dataDir, cacheDir string, p memory.Profile) (mgr uintptr, cleanup func()) {
	cleanup = func() {}
	if !p.Enabled() || m.dataMgrNew3 == nil {
		return a.wkDataManagerNew("base-data-directory", dataDir, "base-cache-directory", cacheDir, 0), cleanup
	}
	tmp, err := os.MkdirTemp("", "krate-cache-")
	if err != nil {
		return a.wkDataManagerNew("base-data-directory", dataDir, "base-cache-directory", cacheDir, 0), cleanup
	}
	cleanup = func() { _ = os.RemoveAll(tmp) }
	return m.dataMgrNew3("base-data-directory", dataDir, "base-cache-directory", cacheDir, "disk-cache-directory", tmp, 0), cleanup
}

// applyContext sets the context and data manager levers of a profile.
func (m *memAPI) applyContext(ctx, mgr uintptr, p memory.Profile, f Features) {
	if !p.Enabled() {
		return
	}
	// The document viewer cache model keeps no resource cache between
	// loads. A desktop app loads its pages from the app origin, so the
	// browser-style cache would only hold memory.
	if m.setCacheModel != nil {
		m.setCacheModel(ctx, cacheModelDocumentViewer)
	}
	// No favicon database (a desktop window has no use for page icons) and
	// no intelligent tracking prevention (it keeps a per-site database and
	// runs in the network process for a single-origin app).
	if m.setFavicons != nil {
		m.setFavicons(mgr, 0)
	}
	if m.setITP != nil {
		m.setITP(mgr, 0)
	}
	// Spell checking loads the enchant dictionaries into the UI process.
	if m.setSpellChecked != nil {
		m.setSpellChecked(ctx, b2i(f.SpellCheck))
	}
}

// applyViewSettings applies the profile to the WebKitSettings of one web
// view. Window code calls it once per view, after creating it. Each
// switched-off feature stops WebKit from initializing the corresponding
// subsystem in the web process (GStreamer pipelines, the audio context
// machinery, the GL context, the WebRTC stack, caches).
func (a *App) applyViewSettings(settings uintptr) {
	if settings == 0 || !a.profile.Enabled() || a.mem == nil {
		return
	}
	f := a.features
	set := func(prop string, on bool) {
		if fn := a.mem.settings[prop]; fn != nil {
			fn(settings, b2i(on))
		}
	}
	set("enable-media", f.Media || f.WebRTC)
	set("enable-mediasource", f.Media)
	set("enable-media-stream", f.WebRTC)
	set("enable-webaudio", f.WebAudio)
	set("enable-webgl", f.WebGL)
	set("enable-webrtc", f.WebRTC)
	set("enable-encrypted-media", f.Media)
	// Not useful in a single-origin desktop app.
	set("enable-dns-prefetching", false)
	set("enable-offline-web-application-cache", false)
	// The page cache keeps whole previous pages alive for back navigation.
	set("enable-page-cache", false)
}
