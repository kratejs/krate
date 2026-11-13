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

func motionCfg(t *testing.T, raw string) *config.MotionConfig {
	t.Helper()
	var m config.MotionConfig
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	return &m
}

func TestMotionChunkSourceContents(t *testing.T) {
	src, err := motionChunkSource(motionCfg(t, `{"reducedMotion":"never","viewport":{"amount":0.4}}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "Motion") || !strings.Contains(src, "krateMotion") {
		t.Fatal("chunk must hold the vendored library and the init code")
	}
	if strings.Contains(src, "__MOTION_CONFIG__") {
		t.Fatal("config placeholder was not replaced")
	}
	if !strings.Contains(src, `"reducedMotion":"never"`) || !strings.Contains(src, `"amount":0.4`) {
		t.Fatal("configured options must be embedded")
	}
	min, err := motionChunkSource(motionCfg(t, `true`), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(min, "krateMotion") {
		t.Fatal("minified chunk lost the init code")
	}
}

func TestMotionHeadHTML(t *testing.T) {
	if motionHeadHTML("", "", true) != "" {
		t.Fatal("no chunk means no markup")
	}
	html := motionHeadHTML("/docs", "chunks/motion.abc.js", true)
	if !strings.Contains(html, `<script defer src="/docs/chunks/motion.abc.js"></script>`) {
		t.Fatalf("script tag missing or not base-prefixed: %s", html)
	}
	if !strings.Contains(html, "krate-motion-pending") {
		t.Fatalf("flash guard missing for declarative mode: %s", html)
	}
	if strings.Contains(motionHeadHTML("", "chunks/motion.abc.js", false), "krate-motion-pending") {
		t.Fatal("flash guard must be omitted when declarative binding is off")
	}
}

func buildMotionSite(t *testing.T, mc *config.MotionConfig) (outDir, html string) {
	t.Helper()
	root := t.TempDir()
	page := filepath.Join(root, "src", "pages", "index.tsx")
	if err := os.MkdirAll(filepath.Dir(page), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "export default function Home() { return <h1 data-motion=\"fade-up\">Hi</h1>; }\n"
	if err := os.WriteFile(page, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Motion = mc
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.OutDir, string(data)
}

var motionScriptRe = regexp.MustCompile(`<script defer src="?(/chunks/motion\.[0-9a-z]+\.js)"?></script>`)

func TestBuildMotionEmitsChunkOnStaticPage(t *testing.T) {
	outDir, html := buildMotionSite(t, motionCfg(t, `{"viewport":{"once":false}}`))
	m := motionScriptRe.FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("page must reference the motion chunk:\n%.1500s", html)
	}
	chunk, err := os.ReadFile(filepath.Join(outDir, filepath.FromSlash(strings.TrimPrefix(m[1], "/"))))
	if err != nil {
		t.Fatalf("referenced chunk was not written: %v", err)
	}
	if !strings.Contains(string(chunk), "krateMotion") || !strings.Contains(string(chunk), "once:!1") {
		t.Fatal("chunk must carry the library, the init code and the configured options")
	}
	if !strings.Contains(html, "krate-motion-pending") {
		t.Fatal("page must carry the flash guard")
	}
}

func TestBuildWithoutMotionShipsNoMotion(t *testing.T) {
	outDir, html := buildMotionSite(t, nil)
	if motionScriptRe.MatchString(html) || strings.Contains(html, "krate-motion") {
		t.Fatalf("motion must not appear when unset:\n%.1500s", html)
	}
	if matches, _ := filepath.Glob(filepath.Join(outDir, "chunks", "motion.*")); len(matches) != 0 {
		t.Fatalf("no motion chunk expected, got %v", matches)
	}
	_, html = buildMotionSite(t, motionCfg(t, `false`))
	if strings.Contains(html, "krate-motion") || motionScriptRe.MatchString(html) {
		t.Fatal("motion:false must ship no motion")
	}
}

func TestBuildMotionNotDeclarativeSkipsFlashGuard(t *testing.T) {
	_, html := buildMotionSite(t, motionCfg(t, `{"declarative":false}`))
	if !motionScriptRe.MatchString(html) {
		t.Fatal("library must still ship when declarative is off")
	}
	if strings.Contains(html, "krate-motion-pending") {
		t.Fatal("no flash guard without declarative binding")
	}
}

const motionStubHarness = `
const calls = [];
const handlers = {};
const warns = [];
globalThis.window = globalThis;
globalThis.console = { warn: (m) => warns.push(String(m)), log: console.log };
const reduceQuery = { matches: false };
globalThis.matchMedia = () => reduceQuery;
globalThis.getComputedStyle = () => ({ getPropertyValue: () => "", color: "rgb(0, 0, 0)" });
globalThis.addEventListener = (n, f) => { handlers[n] = f; };
globalThis.Motion = {
  animate: (el, target, opts) => { calls.push({ type: "animate", id: el.id, target, opts }); return { id: el.id }; },
  inView: (el, cb, opts) => { calls.push({ type: "inView", id: el.id, opts }); el.enter = cb; return () => {}; },
  hover: (el, cb) => { el.hoverStart = cb; return () => {}; },
  press: (el, cb) => { el.pressStart = cb; return () => {}; },
  scroll: (a, opts) => { calls.push({ type: "scroll", id: a.id, opts }); return () => {}; },
  stagger: (d, o) => (i, n) => i * d,
};
function makeEl(id, attrs, kids) {
  return {
    id, nodeType: 1, attrs: Object.assign({}, attrs), kids: kids || [],
    getAttribute(k) { return k in this.attrs ? this.attrs[k] : null; },
    hasAttribute(k) { return k in this.attrs; },
    setAttribute(k, v) { this.attrs[k] = v; },
    matches() { return true; },
    contains(o) { return this.kids.indexOf(o) >= 0; },
    querySelectorAll() { return this.kids; },
  };
}
const classes = [];
let page = [];
let containers = [];
globalThis.document = {
  readyState: "complete",
  documentElement: { classList: { add: (c) => classes.push(c), remove: (c) => classes.push("-" + c) } },
  querySelectorAll: (sel) => (sel === "[data-motion-stagger]" ? containers : page),
  addEventListener() {},
};
`

const motionStubScenarios = `
const out = {};
const byId = (id, type) => calls.filter((c) => c.id === id && (!type || c.type === type));

const a = makeEl("a", { "data-motion": "fade-up" });
const b = makeEl("b", { "data-motion": "pop", "data-motion-trigger": "load", "data-motion-delay": "0.3" });
const hov = makeEl("h", { "data-motion-hover": '{"scale":1.1}' });
const s1 = makeEl("s1", { "data-motion": "fade" });
const s2 = makeEl("s2", { "data-motion": "fade" });
const s3 = makeEl("s3", { "data-motion": "fade" });
const box = makeEl("box", { "data-motion-stagger": "0.1" }, [s1, s2, s3]);
const bad = makeEl("bad", { "data-motion": "nope", "data-motion-animate": "{broken" });
const scr = makeEl("scr", { "data-motion-scroll-animate": '{"opacity":[0,1]}', "data-motion-scroll-offset": '["start end","end start"]' });
page = [a, b, hov, s1, s2, s3, box, bad, scr];
containers = [box];

__INIT__

out.hasApi = typeof globalThis.krateMotion === "object" && typeof krateMotion.refresh === "function";
out.readyClass = classes.indexOf("krate-motion-ready") >= 0 && classes.indexOf("-krate-motion-pending") >= 0;
out.aInitial = byId("a", "animate")[0];
out.aInView = byId("a", "inView")[0];
a.enter();
out.aEnter = byId("a", "animate")[1];
out.bCalls = byId("b", "animate");
out.bInView = byId("b", "inView").length;
const leave = hov.hoverStart();
out.hoverIn = byId("h", "animate").pop();
leave();
out.hoverOut = byId("h", "animate").pop();
out.staggerDelays = [s1, s2, s3].map((el) => {
  el.enter();
  const c = byId(el.id, "animate");
  return c[c.length - 1].opts.delay || 0;
});
out.badBound = bad.hasAttribute("data-motion-bound");
out.badAnimates = byId("bad", "animate").length;
out.warns = warns.length;
out.scroll = calls.filter((c) => c.type === "scroll")[0];
out.ready = a.hasAttribute("data-motion-ready");
const before = calls.length;
krateMotion.refresh();
out.refreshAddedCalls = calls.length - before;
reduceQuery.matches = true;
const r = makeEl("r", { "data-motion": "fade-up" });
page = [r];
containers = [];
krateMotion.refresh();
out.reducedInitial = byId("r", "animate")[0];
reduceQuery.matches = false;
const n = makeEl("n", { "data-motion": "zoom-in" });
page = [n];
handlers["krate:navigate-end"]();
out.navBound = n.hasAttribute("data-motion-bound");
out.presetNames = Object.keys(krateMotion.presets);
out.easeCustom = krateMotion.ease("snappy");
out.easeBezier = krateMotion.ease("cubic-bezier(0.1, 0.2, 0.3, 0.4)");
out.easeNamed = krateMotion.ease("easeOut");
console.log(JSON.stringify(out));
`

func TestMotionInitRunsAgainstAMotionStub(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		requireE2E(t, "node not found")
		return
	}
	mc := motionCfg(t, `{
		"transition": {"type":"spring","stiffness":200,"ease":"snappy"},
		"viewport": {"amount":0.4,"margin":"0px 0px -10%","once":true},
		"easings": {"snappy":[0.2,0.9,0.3,1]},
		"presets": {"pop":{"initial":{"scale":0.5},"animate":{"scale":1}}}
	}`)
	init, err := motionInitSource(mc, false)
	if err != nil {
		t.Fatal(err)
	}
	script := motionStubHarness + strings.Replace(motionStubScenarios, "__INIT__", init, 1)
	f := filepath.Join(t.TempDir(), "init.js")
	if err := os.WriteFile(f, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	outBytes, err := exec.Command(node, f).CombinedOutput()
	if err != nil {
		t.Fatalf("node failed: %v\n%s", err, outBytes)
	}
	type call struct {
		Target map[string]any
		Opts   map[string]any
	}
	var got struct {
		HasApi            bool
		ReadyClass        bool
		AInitial          call
		AInView           call
		AEnter            call
		BCalls            []call
		BInView           int
		HoverIn           call
		HoverOut          call
		StaggerDelays     []float64
		BadBound          bool
		BadAnimates       int
		Warns             int
		Scroll            call
		Ready             bool
		RefreshAddedCalls int
		ReducedInitial    call
		NavBound          bool
		PresetNames       []string
		EaseCustom        []float64
		EaseBezier        []float64
		EaseNamed         string
	}
	if err := json.Unmarshal(outBytes, &got); err != nil {
		t.Fatalf("bad output %q: %v", outBytes, err)
	}
	if !got.HasApi || !got.ReadyClass || !got.Ready {
		t.Fatalf("init did not expose the API or mark readiness: %+v", got)
	}
	if got.AInitial.Target["opacity"] != 0.0 || got.AInitial.Target["y"] != 24.0 || got.AInitial.Opts["duration"] != 0.0 {
		t.Fatalf("preset initial state must apply instantly: %+v", got.AInitial)
	}
	if got.AInView.Opts["amount"] != 0.4 || got.AInView.Opts["margin"] != "0px 0px -10%" {
		t.Fatalf("viewport defaults must come from config: %+v", got.AInView)
	}
	if got.AEnter.Target["opacity"] != 1.0 || got.AEnter.Target["y"] != 0.0 {
		t.Fatalf("entering the viewport must play the preset: %+v", got.AEnter)
	}
	if got.AEnter.Opts["type"] != "spring" || got.AEnter.Opts["stiffness"] != 200.0 {
		t.Fatalf("default transition must come from config: %+v", got.AEnter.Opts)
	}
	ease, _ := got.AEnter.Opts["ease"].([]any)
	if len(ease) != 4 || ease[0] != 0.2 || ease[3] != 1.0 {
		t.Fatalf("custom easing name must resolve to a cubic bezier: %v", got.AEnter.Opts["ease"])
	}
	if got.BInView != 0 || len(got.BCalls) != 2 || got.BCalls[1].Target["scale"] != 1.0 || got.BCalls[1].Opts["delay"] != 0.3 {
		t.Fatalf("custom preset with load trigger and delay wrong: inView=%d calls=%+v", got.BInView, got.BCalls)
	}
	if got.HoverIn.Target["scale"] != 1.1 || got.HoverOut.Target["scale"] != 1.0 {
		t.Fatalf("hover must animate in and restore the rest value: in=%+v out=%+v", got.HoverIn, got.HoverOut)
	}
	if len(got.StaggerDelays) != 3 || got.StaggerDelays[0] != 0 || got.StaggerDelays[1] != 0.1 || got.StaggerDelays[2] != 0.2 {
		t.Fatalf("stagger container must offset children: %v", got.StaggerDelays)
	}
	if !got.BadBound || got.BadAnimates != 0 || got.Warns < 1 {
		t.Fatalf("an unknown preset or broken JSON must warn and animate nothing: %+v", got)
	}
	if got.Scroll.Opts["target"] == nil || got.Scroll.Opts["offset"] == nil {
		t.Fatalf("scroll-linked animation must target the element with offsets: %+v", got.Scroll)
	}
	if got.RefreshAddedCalls != 0 {
		t.Fatalf("refresh must not rebind elements, added %d calls", got.RefreshAddedCalls)
	}
	if _, y := got.ReducedInitial.Target["y"]; y || got.ReducedInitial.Target["opacity"] != 0.0 {
		t.Fatalf("reduced motion must drop transforms and keep opacity: %+v", got.ReducedInitial)
	}
	if !got.NavBound {
		t.Fatal("elements added by a soft navigation must be bound")
	}
	if len(got.PresetNames) < 16 || !hasString(got.PresetNames, "pop") || !hasString(got.PresetNames, "fade-up") {
		t.Fatalf("built-in and custom presets must both be available: %v", got.PresetNames)
	}
	if len(got.EaseBezier) != 4 || got.EaseBezier[0] != 0.1 || got.EaseNamed != "easeOut" || len(got.EaseCustom) != 4 {
		t.Fatalf("easing resolution wrong: %+v", got)
	}
}

func hasString(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}
