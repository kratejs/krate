package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodeSmooth(t *testing.T, src string) *Config {
	t.Helper()
	cfg := &Config{}
	if err := json.Unmarshal([]byte(src), cfg); err != nil {
		t.Fatalf("unmarshal %s: %v", src, err)
	}
	return cfg
}

func TestSmoothScrollBoolForms(t *testing.T) {
	if decodeSmooth(t, `{}`).SmoothScrollEnabled() {
		t.Fatal("smooth scroll must be off by default")
	}
	if !decodeSmooth(t, `{"smoothScroll":true}`).SmoothScrollEnabled() {
		t.Fatal("true must enable smooth scroll")
	}
	if decodeSmooth(t, `{"smoothScroll":false}`).SmoothScrollEnabled() {
		t.Fatal("false must leave smooth scroll off")
	}
}

func TestSmoothScrollObjectEnablesAndParses(t *testing.T) {
	cfg := decodeSmooth(t, `{"smoothScroll":{"lerp":0.08,"duration":1.2,"easing":"easeOutCubic","smoothWheel":false,"syncTouch":true,"orientation":"horizontal","anchors":{"offset":-80},"snap":{"type":"mandatory","debounce":200}}}`)
	s := cfg.SmoothScroll
	if !cfg.SmoothScrollEnabled() {
		t.Fatal("an options object must enable smooth scroll")
	}
	if s.Lerp == nil || *s.Lerp != 0.08 || s.Duration == nil || *s.Duration != 1.2 {
		t.Fatalf("lerp/duration not parsed: %+v", s)
	}
	if s.SmoothWheel == nil || *s.SmoothWheel || !s.SyncTouch || s.Orientation != "horizontal" {
		t.Fatalf("flags not parsed: %+v", s)
	}
	if s.Anchors == nil || !s.Anchors.Enabled || s.Anchors.Offset == nil || *s.Anchors.Offset != -80 {
		t.Fatalf("anchors not parsed: %+v", s.Anchors)
	}
	if s.Snap == nil || !s.Snap.Enabled || s.Snap.Type != "mandatory" || *s.Snap.Debounce != 200 {
		t.Fatalf("snap not parsed: %+v", s.Snap)
	}
}

func TestSmoothScrollEnabledFalseInObject(t *testing.T) {
	if decodeSmooth(t, `{"smoothScroll":{"enabled":false,"lerp":0.2}}`).SmoothScrollEnabled() {
		t.Fatal("enabled:false must disable an options object")
	}
}

func TestSmoothScrollAnchorsAndSnapBools(t *testing.T) {
	cfg := decodeSmooth(t, `{"smoothScroll":{"anchors":true,"snap":false}}`)
	if !cfg.SmoothScroll.Anchors.Enabled {
		t.Fatal("anchors:true must enable anchors")
	}
	if cfg.SmoothScroll.Snap.Enabled {
		t.Fatal("snap:false must leave snap off")
	}
}

func TestSmoothScrollValidate(t *testing.T) {
	cases := map[string]string{
		`{"smoothScroll":{"lerp":0}}`:                           "smoothScroll.lerp",
		`{"smoothScroll":{"lerp":1.5}}`:                         "smoothScroll.lerp",
		`{"smoothScroll":{"duration":-1}}`:                      "smoothScroll.duration",
		`{"smoothScroll":{"easing":"bouncy"}}`:                  "unknown easing",
		`{"smoothScroll":{"easing":"cubic-bezier(2,0,0.5,1)"}}`: "between 0 and 1",
		`{"smoothScroll":{"orientation":"diagonal"}}`:           "orientation",
		`{"smoothScroll":{"gestureOrientation":"left"}}`:        "gestureOrientation",
		`{"smoothScroll":{"snap":{"type":"sticky"}}}`:           "snap.type",
		`{"smoothScroll":{"snap":{"distanceThreshold":"far"}}}`: "distanceThreshold",
		`{"smoothScroll":{"anchors":{"easing":"nope"}}}`:        "anchors.easing",
		`{"smoothScroll":{"wheelMultiplier":-2}}`:               "wheelMultiplier",
		`{"smoothScroll":{"snap":{"debounce":-1}}}`:             "snap.debounce",
	}
	for src, want := range cases {
		_, err := decodeSmooth(t, src).Validate()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want error containing %q, got %v", src, want, err)
		}
	}
	valid := []string{
		`{"smoothScroll":true}`,
		`{"smoothScroll":{"lerp":0.1,"duration":1.5,"easing":"cubic-bezier(0.25, 0.1, 0.25, 1)"}}`,
		`{"smoothScroll":{"snap":{"type":"lock","distanceThreshold":"30%","debounce":0}}}`,
		`{"smoothScroll":{"enabled":false,"lerp":9}}`,
	}
	for _, src := range valid {
		if _, err := decodeSmooth(t, src).Validate(); err != nil {
			t.Errorf("%s: unexpected error %v", src, err)
		}
	}
}

func TestSmoothScrollKnownKeys(t *testing.T) {
	if !knownTopLevelKeys["smoothScroll"] {
		t.Fatal("smoothScroll must be a known top-level key")
	}
	for _, key := range []string{"lerp", "duration", "easing", "smoothWheel", "syncTouch", "infinite", "anchors", "snap", "prevent"} {
		if !knownNestedKeys["smoothScroll"][key] {
			t.Errorf("smoothScroll.%s must be a known nested key", key)
		}
	}
	for _, form := range []string{`{"smoothScroll":true}`, `{"smoothScroll":{"lerp":0.1,"anchors":true,"snap":{"type":"lock"}}}`} {
		warnings := UnknownKeyWarnings([]byte(form))
		if len(warnings) != 0 {
			t.Errorf("%s: unexpected warnings %v", form, warnings)
		}
	}
	typo := UnknownKeyWarnings([]byte(`{"smoothScroll":{"lerpp":0.1,"snap":{"tpye":"lock"}}}`))
	if len(typo) != 2 {
		t.Errorf("want 2 unknown-key warnings for typos, got %v", typo)
	}
}

func TestSmoothScrollInTSConfig(t *testing.T) {
	cfg := &Config{}
	if err := parseTSConfig(`export default { smoothScroll: { lerp: 0.2, snap: true } }`, cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.SmoothScrollEnabled() || *cfg.SmoothScroll.Lerp != 0.2 || !cfg.SmoothScroll.Snap.Enabled {
		t.Fatalf("ts config not applied: %+v", cfg.SmoothScroll)
	}
}
