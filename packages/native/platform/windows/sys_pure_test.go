package windows

import (
	"testing"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

func TestPowerEvent(t *testing.T) {
	if k, ok := powerEvent(pbtAPMSuspend, false); !ok || k != caps.PowerSuspend {
		t.Errorf("suspend: %v %v", k, ok)
	}
	if k, ok := powerEvent(pbtAPMResumeAuto, true); !ok || k != caps.PowerResume {
		t.Errorf("resume: %v %v", k, ok)
	}
	// The second resume message of one wake-up (after the user touches the
	// machine) is not a second event.
	if _, ok := powerEvent(pbtAPMResumeSuspend, false); ok {
		t.Error("a resume without a suspend must not be reported")
	}
	if _, ok := powerEvent(0x000A, true); ok {
		t.Error("PBT_POWERSETTINGCHANGE is not a suspend or resume")
	}
}

func TestSessionEvent(t *testing.T) {
	if k, ok := sessionEvent(wtsSessionLock); !ok || k != caps.PowerLock {
		t.Errorf("lock: %v", k)
	}
	if k, ok := sessionEvent(wtsSessionUnlock); !ok || k != caps.PowerUnlock {
		t.Errorf("unlock: %v", k)
	}
	if _, ok := sessionEvent(0x5); ok { // WTS_REMOTE_CONNECT
		t.Error("remote connect is neither")
	}
}

func TestThemeSettingChange(t *testing.T) {
	if !themeSettingChange(0, "ImmersiveColorSet") || !themeSettingChange(spiSetHighContrast, "") {
		t.Error("theme changes not recognized")
	}
	if themeSettingChange(0x2F, "intl") {
		t.Error("an unrelated setting is not a theme change")
	}
}

func TestExecutionFlags(t *testing.T) {
	if f := executionFlags(0, 0); f != esContinuous {
		t.Errorf("none: %#x", f)
	}
	if f := executionFlags(1, 0); f != esContinuous|esSystemRequired {
		t.Errorf("system: %#x", f)
	}
	if f := executionFlags(0, 2); f != esContinuous|esSystemRequired|esDisplayRequire {
		t.Errorf("display: %#x", f)
	}
}

func TestTaskbarFlags(t *testing.T) {
	for s, want := range map[caps.ProgressState]uint32{
		caps.ProgressNone: 0, caps.ProgressIndeterminate: 1, caps.ProgressNormal: 2, caps.ProgressError: 4, caps.ProgressPaused: 8,
	} {
		if got := taskbarFlags(s); got != want {
			t.Errorf("%s = %d, want %d", s, got, want)
		}
	}
}

func TestProcessFailedReason(t *testing.T) {
	for kind, want := range map[int32]struct {
		reason string
		report bool
	}{
		0: {"crashed", true}, 1: {"crashed", true}, 2: {"unresponsive", true}, 3: {"crashed", true},
		4: {"", false}, 5: {"", false}, 6: {"", false}, 9: {"", false},
	} {
		r, ok := processFailedReason(kind)
		if r != want.reason || ok != want.report {
			t.Errorf("kind %d = %q %v", kind, r, ok)
		}
	}
}

func TestDisplayHelpers(t *testing.T) {
	if displayID(`\\.\DISPLAY2`) != "DISPLAY2" || displayID("x") != "x" {
		t.Error("displayID")
	}
	if dpiScale(96) != 1 || dpiScale(144) != 1.5 || dpiScale(0) != 1 {
		t.Error("dpiScale")
	}
}

func TestIdleMillisWraps(t *testing.T) {
	if idleMillis(5000, 2000) != 3000 {
		t.Error("plain")
	}
	// The tick count wrapped after the last input.
	if got := idleMillis(100, 0xFFFFFF9C); got != 200 {
		t.Errorf("wrapped: %d", got)
	}
}
