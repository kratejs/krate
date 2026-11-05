package jsvalue

import (
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// buildClock is the instant `new Date()` and `Date.now()` evaluate to while
// a site is built. One build uses one instant, so two components rendered
// seconds apart agree.
var buildClock atomic.Pointer[time.Time]

// SourceDateEpoch returns the time named by the SOURCE_DATE_EPOCH environment
// variable (integer seconds since the Unix epoch), the convention reproducible
// builds use. ok=false when it is unset or not an integer.
func SourceDateEpoch() (time.Time, bool) {
	s := strings.TrimSpace(os.Getenv("SOURCE_DATE_EPOCH"))
	if s == "" {
		return time.Time{}, false
	}
	secs, err := strconv.ParseInt(s, 10, 64)
	if err != nil || secs < -8_640_000_000_000 || secs > 8_640_000_000_000 {
		return time.Time{}, false
	}
	return time.Unix(secs, 0).UTC(), true
}

// StartBuildClock fixes the build instant: SOURCE_DATE_EPOCH when it is set,
// otherwise the current time. Call it at the start of each build.
func StartBuildClock() time.Time {
	t, ok := SourceDateEpoch()
	if !ok {
		t = time.Now().UTC()
	}
	t = t.Truncate(time.Millisecond)
	buildClock.Store(&t)
	return t
}

// SetBuildClock fixes the build instant to t (tests).
func SetBuildClock(t time.Time) {
	t = t.UTC().Truncate(time.Millisecond)
	buildClock.Store(&t)
}

// ResetBuildClock goes back to reading the live clock.
func ResetBuildClock() { buildClock.Store(nil) }

// nowMS is the current time as a JS time value (milliseconds since the epoch).
func nowMS() float64 {
	if t := buildClock.Load(); t != nil {
		return float64(t.UnixMilli())
	}
	if t, ok := SourceDateEpoch(); ok {
		return float64(t.UnixMilli())
	}
	return float64(time.Now().UnixMilli())
}

// BuildNowMS is the build instant as a JS time value, for hosts that hand
// expressions to another engine and want it to agree with this package.
func BuildNowMS() float64 { return nowMS() }
