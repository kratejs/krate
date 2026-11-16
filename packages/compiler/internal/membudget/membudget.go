// Package membudget sizes the memory a build may use and slows the build down
// when it nears that budget, so a build never drives the machine out of
// memory.
//
// The budget covers the krate process and its build-time JavaScript workers.
// KRATE_MAX_MEMORY sets it; otherwise it is three quarters of the memory
// available when the build starts (the cgroup's headroom in a container).
package membudget

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
)

// EnvVar overrides the budget: a size ("6G", "4096MiB", "2.5gb", bytes) or a
// percentage of the machine's or container's memory ("50%").
const EnvVar = "KRATE_MAX_MEMORY"

const (
	kib = int64(1) << 10
	mib = int64(1) << 20
	gib = int64(1) << 30

	// minBudget is the smallest budget used: below it every heavy job runs
	// alone anyway.
	minBudget = 256 * mib
	// fallbackBudget is the budget where the available memory is unknown.
	fallbackBudget = 4 * gib
)

// Budget is the memory a build may use.
type Budget struct {
	// Bytes is the budget.
	Bytes int64
	// Total is the memory of the machine or container, 0 when unknown.
	Total int64
	// Source tells where Bytes came from: EnvVar, "cgroup", "available" or
	// "default".
	Source string
}

// system is what the machine (or the container) reports.
type system struct {
	total, avail int64 // 0 when unknown
	cgroup       bool  // a cgroup limit bounds total or avail
	guessed      bool  // avail is an estimate, not a measurement
}

// Detect works out the budget from the environment and the system. err
// reports a KRATE_MAX_MEMORY that could not be parsed; the budget then comes
// from the system.
func Detect() (Budget, error) {
	return detect(os.Getenv(EnvVar), readSystem())
}

func detect(env string, sys system) (Budget, error) {
	if env = strings.TrimSpace(env); env != "" {
		n, err := ParseSize(env, sys.total)
		if err == nil {
			return Budget{Bytes: max(n, minBudget), Total: sys.total, Source: EnvVar}, nil
		}
		b, _ := detect("", sys)
		return b, fmt.Errorf("%s=%q: %w", EnvVar, env, err)
	}
	if sys.avail <= 0 {
		n := int64(fallbackBudget)
		if sys.total > 0 {
			n = min(n, sys.total/2)
		}
		return Budget{Bytes: max(n, minBudget), Total: sys.total, Source: "default"}, nil
	}
	src := "available"
	if sys.cgroup {
		src = "cgroup"
	}
	return Budget{Bytes: max(sys.avail/4*3, minBudget), Total: sys.total, Source: src}, nil
}

// ParseSize parses a size: bytes, a number with a unit (K, M, G, T, with an
// optional "i" and "B", any case; powers of 1024) or a percentage of total.
func ParseSize(s string, total int64) (int64, error) {
	s = strings.TrimSpace(s)
	if pct, ok := strings.CutSuffix(s, "%"); ok {
		if total <= 0 {
			return 0, fmt.Errorf("a percentage needs the machine's memory size, which is unknown here")
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(pct), 64)
		if err != nil || f <= 0 || f > 100 || math.IsNaN(f) {
			return 0, fmt.Errorf("want a percentage above 0 and at most 100")
		}
		return int64(float64(total) * f / 100), nil
	}
	i := strings.IndexFunc(s, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	num, unit := s, ""
	if i >= 0 {
		num, unit = s[:i], strings.ToLower(strings.TrimSpace(s[i:]))
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil || f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("want a size like 4G, 4096M or 50%%")
	}
	unit = strings.TrimSuffix(strings.TrimSuffix(unit, "b"), "i")
	mult := map[string]int64{"": 1, "k": kib, "m": mib, "g": gib, "t": 1 << 40}[unit]
	if mult == 0 {
		return 0, fmt.Errorf("unknown unit in %q (use K, M, G or T)", s)
	}
	v := f * float64(mult)
	if v >= math.MaxInt64 {
		return 0, fmt.Errorf("size %q is too large", s)
	}
	return int64(v), nil
}

// Format renders n bytes for messages.
func Format(n int64) string {
	switch {
	case n >= gib:
		return strconv.FormatFloat(float64(n)/float64(gib), 'f', 1, 64) + " GiB"
	case n >= mib:
		return strconv.FormatInt(n/mib, 10) + " MiB"
	}
	return strconv.FormatInt(n/kib, 10) + " KiB"
}

var (
	currentOnce sync.Once
	current     Budget
	currentErr  error
)

// Current returns the process's budget, detected once.
func Current() (Budget, error) {
	currentOnce.Do(func() { current, currentErr = Detect() })
	return current, currentErr
}

// SetCurrent replaces the process's budget (tests, and a CLI flag if one is
// added).
func SetCurrent(b Budget) {
	currentOnce.Do(func() {})
	current, currentErr = b, nil
	defaultMonitor.reset()
}
