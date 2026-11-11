package windows

import (
	"reflect"
	"strings"
	"testing"
)

func TestMemoryProfileNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"": MemoryBalanced, "balanced": MemoryBalanced, "minimal": MemoryMinimal,
		"performance": MemoryPerformance, "Minimal": MemoryBalanced, "bogus": MemoryBalanced,
	} {
		if got := memoryProfile(in); got != want {
			t.Errorf("memoryProfile(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBrowserArguments(t *testing.T) {
	used := map[string]bool{"media": true, "webaudio": true, "webgl": true, "webrtc": true, "spellcheck": true}
	feat := "--disable-features=SpareRendererForSitePerProcess,BackForwardCache,msWebOOUI,msPdfOOUI"
	base := []string{"--renderer-process-limit=1", feat}
	minimal := append(append([]string{}, base...), "--js-flags=--max-old-space-size=512", "--disable-gpu-shader-disk-cache")
	cases := []struct {
		name     string
		profile  string
		features map[string]bool
		want     []string
	}{
		{"performance", "performance", nil, nil},
		{"performance ignores features", "performance", map[string]bool{}, nil},
		{"default is balanced", "", nil, base},
		{"unknown is balanced", "weird", nil, base},
		{"balanced, nothing detected", "balanced", map[string]bool{}, append(append([]string{}, base...), "--disable-webgl")},
		{"balanced, everything used", "balanced", used, base},
		{"balanced, webgl only", "balanced", map[string]bool{"webgl": true}, base},
		{"minimal, unknown features", "minimal", nil, minimal},
		{"minimal, nothing detected", "minimal", map[string]bool{}, append(append([]string{}, minimal...), "--disable-webgl")},
		{"minimal, everything used", "minimal", used, minimal},
	}
	for _, c := range cases {
		got := browserArguments(c.profile, c.features)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
		if s := browserArgumentString(c.profile, c.features); s != strings.Join(c.want, " ") {
			t.Errorf("%s: string %q", c.name, s)
		}
	}
}

// TestBrowserArgumentsSafety pins what must never be added.
func TestBrowserArgumentsSafety(t *testing.T) {
	for _, p := range []string{"balanced", "minimal", "performance"} {
		s := browserArgumentString(p, map[string]bool{})
		for _, bad := range []string{"msSmartScreenProtection", "--disable-gpu ", "--disable-background-networking", "--single-process", "--disable-web-security"} {
			if strings.Contains(s+" ", bad) {
				t.Errorf("%s: contains %q: %s", p, bad, s)
			}
		}
		if strings.Count(s, "--disable-features=") > 1 || strings.Count(s, "--js-flags=") > 1 {
			t.Errorf("%s: repeated switch: %s", p, s)
		}
	}
}

func TestMediaCapturePolicy(t *testing.T) {
	cases := []struct {
		profile  string
		features map[string]bool
		deny     bool
	}{
		{"balanced", nil, false},
		{"balanced", map[string]bool{}, true},
		{"balanced", map[string]bool{"webrtc": true}, false},
		{"minimal", map[string]bool{"media": true}, true},
		{"performance", map[string]bool{}, false},
	}
	for _, c := range cases {
		if got := denyMediaCapture(c.profile, c.features); got != c.deny {
			t.Errorf("denyMediaCapture(%s, %v) = %v", c.profile, c.features, got)
		}
	}
	for kind, want := range map[uint32]bool{0: false, permKindMicrophone: true, permKindCamera: true, 3: false, 4: false} {
		if got := deniedPermission(true, kind); got != want {
			t.Errorf("kind %d: %v", kind, got)
		}
		if deniedPermission(false, kind) {
			t.Errorf("kind %d denied while policy off", kind)
		}
	}
}

func TestVisibleMemoryTarget(t *testing.T) {
	for p, want := range map[string]int{"minimal": memTargetLow, "balanced": memTargetNormal, "performance": memTargetNormal, "": memTargetNormal} {
		if got := visibleMemoryTarget(p); got != want {
			t.Errorf("%q: %d", p, got)
		}
	}
}

// TestMemoryVtableLayout pins the memory-related vtables against WebView2.h.
func TestMemoryVtableLayout(t *testing.T) {
	counts := map[string]int{
		"ICoreWebView2EnvironmentOptions":              11,
		"ICoreWebView2PermissionRequestedEventHandler": 4,
		"ICoreWebView2PermissionRequestedEventArgs":    9,
		"ICoreWebView2_19":                             121,
	}
	for name, n := range counts {
		ms := ifaceMethods[name]
		if len(ms) != n {
			t.Errorf("%s: %d methods, want %d", name, len(ms), n)
		}
		if len(ms) >= 3 && (ms[0] != "QueryInterface" || ms[1] != "AddRef" || ms[2] != "Release") {
			t.Errorf("%s: IUnknown slots wrong", name)
		}
	}
	// ICoreWebView2_19 inherits ICoreWebView2 slot for slot.
	base, ext := ifaceMethods["ICoreWebView2"], ifaceMethods["ICoreWebView2_19"]
	if !reflect.DeepEqual(base, ext[:len(base)]) {
		t.Error("ICoreWebView2_19 does not start with the ICoreWebView2 table")
	}
	opts := []string{
		"QueryInterface", "AddRef", "Release",
		"get_AdditionalBrowserArguments", "put_AdditionalBrowserArguments",
		"get_Language", "put_Language",
		"get_TargetCompatibleBrowserVersion", "put_TargetCompatibleBrowserVersion",
		"get_AllowSingleSignOnUsingOSPrimaryAccount", "put_AllowSingleSignOnUsingOSPrimaryAccount",
	}
	if !reflect.DeepEqual(ifaceMethods["ICoreWebView2EnvironmentOptions"], opts) {
		t.Errorf("options vtable: %v", ifaceMethods["ICoreWebView2EnvironmentOptions"])
	}
	for _, s := range []struct {
		got, want int
		name      string
	}{
		{optGetArguments, 3, "get_AdditionalBrowserArguments"},
		{wvAddPermissionRequested, 23, "add_PermissionRequested"},
		{permArgsGetPermissionKind, 4, "get_PermissionKind"},
		{permArgsPutState, 7, "put_State"},
		{vtIndex("ICoreWebView2_19", "get_MemoryUsageTargetLevel"), wv19PutMemoryTargetLevel - 1, "get_MemoryUsageTargetLevel"},
		{wv19PutMemoryTargetLevel, len(ext) - 1, "put_MemoryUsageTargetLevel (last slot)"},
	} {
		if s.got != s.want {
			t.Errorf("%s slot %d, want %d", s.name, s.got, s.want)
		}
	}
}

func TestMemoryIIDs(t *testing.T) {
	for _, s := range []string{iidEnvironmentOptions, iidPermissionRequestedHandler, iidWebView2_19} {
		if _, err := parseGUID(s); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	g := mustGUID(iidWebView2_19) // 6921f954-79b0-437f-a997-c85811897c68
	if g[0] != 0x54 || g[3] != 0x69 || g[4] != 0xb0 || g[5] != 0x79 || g[6] != 0x7f || g[7] != 0x43 || g[8] != 0xa9 || g[15] != 0x68 {
		t.Errorf("ICoreWebView2_19 iid: %x", g)
	}
	g = mustGUID(iidEnvironmentOptions) // 2fde08a8-1e9a-4766-8c05-95a9ceb9d1c5
	if g[0] != 0xa8 || g[3] != 0x2f || g[4] != 0x9a || g[5] != 0x1e || g[6] != 0x66 || g[7] != 0x47 || g[8] != 0x8c || g[15] != 0xc5 {
		t.Errorf("options iid: %x", g)
	}
}
