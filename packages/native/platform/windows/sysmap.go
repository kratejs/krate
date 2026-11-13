package windows

import (
	"strings"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Pure helpers of the OS integration glue in system_windows.go.

// Window messages and codes.
const (
	wmSettingChange     = 0x001A
	wmDisplayChange     = 0x007E
	wmPowerBroadcast    = 0x0218
	wmWTSSessionChange  = 0x02B1
	pbtAPMSuspend       = 0x0004
	pbtAPMResumeSuspend = 0x0007
	pbtAPMResumeAuto    = 0x0012
	wtsSessionLock      = 0x0007
	wtsSessionUnlock    = 0x0008
	spiSetHighContrast  = 0x0043

	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
	esDisplayRequire = 0x00000002

	tbpfNoProgress    = 0x0
	tbpfIndeterminate = 0x1
	tbpfNormal        = 0x2
	tbpfError         = 0x4
	tbpfPaused        = 0x8
)

// powerEvent maps WM_POWERBROADCAST's wParam to a power event. resumed says
// whether a resume was already reported for this suspend: Windows sends both
// PBT_APMRESUMEAUTOMATIC and PBT_APMRESUMESUSPEND for one wake-up.
func powerEvent(wParam uintptr, suspended bool) (kind caps.PowerEventKind, ok bool) {
	switch wParam {
	case pbtAPMSuspend:
		return caps.PowerSuspend, true
	case pbtAPMResumeAuto, pbtAPMResumeSuspend:
		if suspended {
			return caps.PowerResume, true
		}
	}
	return "", false
}

// sessionEvent maps WM_WTSSESSION_CHANGE's wParam to a lock or unlock event.
func sessionEvent(wParam uintptr) (caps.PowerEventKind, bool) {
	switch wParam {
	case wtsSessionLock:
		return caps.PowerLock, true
	case wtsSessionUnlock:
		return caps.PowerUnlock, true
	}
	return "", false
}

// themeSettingChange reports whether a WM_SETTINGCHANGE (wParam, and the
// string lParam points to) means the colour theme or the high contrast mode
// changed.
func themeSettingChange(wParam uintptr, area string) bool {
	return area == "ImmersiveColorSet" || wParam == spiSetHighContrast
}

// executionFlags is the SetThreadExecutionState argument for the number of
// system and display blockers.
func executionFlags(system, display int) uint32 {
	f := uint32(esContinuous)
	if system > 0 || display > 0 {
		f |= esSystemRequired
	}
	if display > 0 {
		f |= esDisplayRequire
	}
	return f
}

// taskbarFlags maps a progress state to TBPFLAG values.
func taskbarFlags(s caps.ProgressState) uint32 {
	switch s {
	case caps.ProgressNormal:
		return tbpfNormal
	case caps.ProgressIndeterminate:
		return tbpfIndeterminate
	case caps.ProgressError:
		return tbpfError
	case caps.ProgressPaused:
		return tbpfPaused
	}
	return tbpfNoProgress
}

// processFailedReason maps COREWEBVIEW2_PROCESS_FAILED_KIND to a crash reason.
// report is false for processes that do not end the page (GPU, utility and
// sandbox helpers are restarted by WebView2 itself).
func processFailedReason(kind int32) (reason string, report bool) {
	switch kind {
	case 0: // BROWSER_PROCESS_EXITED
		return "crashed", true
	case 1, 3: // RENDER_PROCESS_EXITED, FRAME_RENDER_PROCESS_EXITED
		return "crashed", true
	case 2: // RENDER_PROCESS_UNRESPONSIVE
		return "unresponsive", true
	}
	return "", false
}

// displayID names a monitor by its device name ("\\.\DISPLAY1" becomes
// "DISPLAY1").
func displayID(device string) string {
	return strings.TrimPrefix(device, `\\.\`)
}

// dpiScale is the scale factor of an effective DPI (96 is 1.0).
func dpiScale(dpi uint32) float64 {
	if dpi == 0 {
		return 1
	}
	return float64(dpi) / 96
}

// idleMillis is the time since the last input given GetTickCount and
// LASTINPUTINFO.dwTime, both 32-bit tick counts that wrap.
func idleMillis(tick, last uint32) uint32 { return tick - last }
