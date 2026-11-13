package build

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func ptr[T any](v T) *T { return &v }

func TestLenisInitForMapsOptions(t *testing.T) {
	c := &config.SmoothScrollConfig{
		Enabled:     true,
		Lerp:        ptr(0.08),
		Duration:    ptr(1.4),
		Easing:      "easeOutCubic",
		SmoothWheel: ptr(false),
		SyncTouch:   true,
		Orientation: "horizontal",
		Prevent:     ".modal",
		Anchors:     &config.SmoothScrollAnchors{Enabled: true, Offset: ptr(-64.0)},
		Snap:        &config.SmoothScrollSnap{Enabled: true, Type: "mandatory", Debounce: ptr(150)},
	}
	got := lenisInitFor(c)
	o := got.Options
	if o["lerp"] != 0.08 || o["duration"] != 1.4 || o["easing"] != "easeOutCubic" || o["smoothWheel"] != false {
		t.Fatalf("scalar options wrong: %v", o)
	}
	if o["syncTouch"] != true || o["orientation"] != "horizontal" || o["prevent"] != ".modal" {
		t.Fatalf("flag options wrong: %v", o)
	}
	anchors, ok := o["anchors"].(map[string]any)
	if !ok || anchors["offset"] != -64.0 {
		t.Fatalf("anchors wrong: %v", o["anchors"])
	}
	if got.Snap["type"] != "mandatory" || got.Snap["debounce"] != 150 {
		t.Fatalf("snap wrong: %v", got.Snap)
	}
	if _, set := o["infinite"]; set {
		t.Fatal("unset options must not be emitted so Lenis defaults apply")
	}
}

func TestLenisInitForBareAnchorsAndNoSnap(t *testing.T) {
	got := lenisInitFor(&config.SmoothScrollConfig{Enabled: true, Anchors: &config.SmoothScrollAnchors{Enabled: true}})
	if got.Options["anchors"] != true {
		t.Fatalf("anchors:true must stay a boolean, got %v", got.Options["anchors"])
	}
	if got.Snap != nil {
		t.Fatal("snap must be absent unless enabled")
	}
}

func TestLenisChunkSourceContents(t *testing.T) {
	plain, err := lenisChunkSource(&config.SmoothScrollConfig{Enabled: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain, "globalThis.Lenis") || !strings.Contains(plain, "new Lenis(") {
		t.Fatal("chunk must hold the vendored library and the init code")
	}
	if strings.Contains(plain, "globalThis.Snap") {
		t.Fatal("snap plugin must not ship unless enabled")
	}
	if strings.Contains(plain, "__LENIS_CONFIG__") {
		t.Fatal("config placeholder was not replaced")
	}
	withSnap, err := lenisChunkSource(&config.SmoothScrollConfig{Enabled: true, Snap: &config.SmoothScrollSnap{Enabled: true}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(withSnap, "globalThis.Snap") {
		t.Fatal("snap plugin must ship when enabled")
	}
}

func TestLenisHeadHTML(t *testing.T) {
	if lenisHeadHTML("", "") != "" {
		t.Fatal("no chunk means no markup")
	}
	html := lenisHeadHTML("/docs", "chunks/lenis.abc.js")
	if !strings.Contains(html, `<script defer src="/docs/chunks/lenis.abc.js"></script>`) {
		t.Fatalf("script tag missing or not base-prefixed: %s", html)
	}
	if !strings.Contains(html, "html.lenis") {
		t.Fatalf("lenis stylesheet missing: %s", html)
	}
}

func buildLenisSite(t *testing.T, sc *config.SmoothScrollConfig) (outDir, html string) {
	t.Helper()
	root := t.TempDir()
	page := filepath.Join(root, "src", "pages", "index.tsx")
	if err := os.MkdirAll(filepath.Dir(page), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(page, []byte("export default function Home() { return <h1>Hi</h1>; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.SmoothScroll = sc
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.OutDir, string(data)
}

var lenisScriptRe = regexp.MustCompile(`<script defer src="?(/chunks/lenis\.[0-9a-z]+\.js)"?></script>`)

func TestBuildSmoothScrollEmitsChunkOnStaticPage(t *testing.T) {
	outDir, html := buildLenisSite(t, &config.SmoothScrollConfig{Enabled: true, Lerp: ptr(0.07)})
	m := lenisScriptRe.FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("page must reference the lenis chunk even with no other client JS:\n%.1500s", html)
	}
	chunk, err := os.ReadFile(filepath.Join(outDir, filepath.FromSlash(strings.TrimPrefix(m[1], "/"))))
	if err != nil {
		t.Fatalf("referenced chunk was not written: %v", err)
	}
	if !strings.Contains(string(chunk), ".07") || !strings.Contains(string(chunk), "Lenis") {
		t.Fatal("chunk must carry the library and the configured options")
	}
	if !strings.Contains(html, "html.lenis") {
		t.Fatal("page must inline the lenis stylesheet")
	}
}

func TestBuildWithoutSmoothScrollShipsNoLenis(t *testing.T) {
	outDir, html := buildLenisSite(t, nil)
	if strings.Contains(html, "lenis") {
		t.Fatalf("lenis must not appear when smoothScroll is unset:\n%.1500s", html)
	}
	matches, _ := filepath.Glob(filepath.Join(outDir, "chunks", "lenis.*"))
	if len(matches) != 0 {
		t.Fatalf("no lenis chunk expected, got %v", matches)
	}
	_, html = buildLenisSite(t, &config.SmoothScrollConfig{Enabled: false})
	if strings.Contains(html, "lenis") {
		t.Fatal("smoothScroll:false must ship no lenis")
	}
}

const lenisStubHarness = `
const seen = {};
globalThis.Lenis = function (o) { seen.opts = o; };
globalThis.Snap = function (l, o) {
  seen.snapOpts = o;
  this.addElement = () => { seen.added = (seen.added || 0) + 1; return () => { seen.removed = (seen.removed || 0) + 1; }; };
};
const els = [{ getAttribute: () => "start center" }, { getAttribute: () => "" }];
globalThis.document = { querySelector: () => null, querySelectorAll: () => els };
const handlers = {};
globalThis.window = globalThis;
globalThis.addEventListener = (n, f) => { handlers[n] = f; };
globalThis.requestAnimationFrame = (f) => f();
`

const lenisStubReport = `
const o = seen.opts;
const out = {
  autoRaf: o.autoRaf,
  easingIsFn: typeof o.easing === "function",
  bezierMid: o.easing(0.5),
  bezierEnds: [o.easing(0), o.easing(1)],
  wrapperDropped: !("wrapper" in o),
  preventHit: o.prevent({ closest: (s) => (s === ".no-smooth" ? {} : null) }),
  preventMiss: o.prevent({ closest: () => null }),
  anchorsEasingIsFn: typeof o.anchors.easing === "function",
  threshold: seen.snapOpts.distanceThreshold,
  addedAfterInit: seen.added,
  hasLenis: !!globalThis.lenis,
  hasSnap: !!globalThis.lenisSnap,
};
handlers["krate:navigate-end"]();
out.addedAfterNav = seen.added;
out.removedAfterNav = seen.removed;
console.log(JSON.stringify(out));
`

func TestLenisInitRunsAgainstALenisStub(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		requireE2E(t, "node not found")
		return
	}
	cfg := &config.SmoothScrollConfig{
		Enabled:  true,
		Easing:   "cubic-bezier(0.25, 0.1, 0.25, 1)",
		Prevent:  ".no-smooth",
		Anchors:  &config.SmoothScrollAnchors{Enabled: true, Easing: "easeInOutCubic"},
		Snap:     &config.SmoothScrollSnap{Enabled: true, DistanceThreshold: "120"},
		Wrapper:  "#missing",
		Duration: ptr(1.0),
	}
	data, _ := json.Marshal(lenisInitFor(cfg))
	init := strings.Replace(lenisInitJS, "__LENIS_CONFIG__", string(data), 1)
	f := filepath.Join(t.TempDir(), "init.js")
	if err := os.WriteFile(f, []byte(lenisStubHarness+init+lenisStubReport), 0o644); err != nil {
		t.Fatal(err)
	}
	outBytes, err := exec.Command(node, f).CombinedOutput()
	if err != nil {
		t.Fatalf("node failed: %v\n%s", err, outBytes)
	}
	var got struct {
		AutoRaf           bool
		EasingIsFn        bool
		BezierMid         float64
		BezierEnds        []float64
		WrapperDropped    bool
		PreventHit        bool
		PreventMiss       bool
		AnchorsEasingIsFn bool
		Threshold         float64
		AddedAfterInit    int
		HasLenis          bool
		HasSnap           bool
		AddedAfterNav     int
		RemovedAfterNav   int
	}
	if err := json.Unmarshal(outBytes, &got); err != nil {
		t.Fatalf("bad output %q: %v", outBytes, err)
	}
	if !got.AutoRaf || !got.EasingIsFn || !got.WrapperDropped || !got.AnchorsEasingIsFn || !got.HasLenis || !got.HasSnap {
		t.Fatalf("init did not set up Lenis as configured: %+v", got)
	}
	if got.BezierEnds[0] != 0 || got.BezierEnds[1] != 1 || got.BezierMid < 0.5 || got.BezierMid > 0.9 {
		t.Fatalf("cubic-bezier easing is wrong: mid %v ends %v", got.BezierMid, got.BezierEnds)
	}
	if !got.PreventHit || got.PreventMiss {
		t.Fatalf("prevent selector not applied: %+v", got)
	}
	if got.Threshold != 120 {
		t.Fatalf("pixel distanceThreshold must become a number, got %v", got.Threshold)
	}
	if got.AddedAfterInit != 2 || got.AddedAfterNav != 4 || got.RemovedAfterNav != 2 {
		t.Fatalf("snap elements must register at load and re-sync after navigation: %+v", got)
	}
}
