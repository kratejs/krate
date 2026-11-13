package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type SmoothScrollConfig struct {
	Enabled               bool                 `json:"enabled,omitempty"`
	Lerp                  *float64             `json:"lerp,omitempty"`
	Duration              *float64             `json:"duration,omitempty"`
	Easing                string               `json:"easing,omitempty"`
	SmoothWheel           *bool                `json:"smoothWheel,omitempty"`
	SyncTouch             bool                 `json:"syncTouch,omitempty"`
	SyncTouchLerp         *float64             `json:"syncTouchLerp,omitempty"`
	TouchInertiaExponent  *float64             `json:"touchInertiaExponent,omitempty"`
	Infinite              bool                 `json:"infinite,omitempty"`
	Orientation           string               `json:"orientation,omitempty"`
	GestureOrientation    string               `json:"gestureOrientation,omitempty"`
	TouchMultiplier       *float64             `json:"touchMultiplier,omitempty"`
	WheelMultiplier       *float64             `json:"wheelMultiplier,omitempty"`
	AutoResize            *bool                `json:"autoResize,omitempty"`
	Overscroll            *bool                `json:"overscroll,omitempty"`
	AutoRaf               *bool                `json:"autoRaf,omitempty"`
	Anchors               *SmoothScrollAnchors `json:"anchors,omitempty"`
	AutoToggle            bool                 `json:"autoToggle,omitempty"`
	AllowNestedScroll     bool                 `json:"allowNestedScroll,omitempty"`
	NaiveDimensions       bool                 `json:"naiveDimensions,omitempty"`
	StopInertiaOnNavigate bool                 `json:"stopInertiaOnNavigate,omitempty"`
	RespectReducedMotion  *bool                `json:"respectReducedMotion,omitempty"`
	Prevent               string               `json:"prevent,omitempty"`
	Wrapper               string               `json:"wrapper,omitempty"`
	Content               string               `json:"content,omitempty"`
	EventsTarget          string               `json:"eventsTarget,omitempty"`
	Snap                  *SmoothScrollSnap    `json:"snap,omitempty"`
}

type SmoothScrollAnchors struct {
	Enabled   bool     `json:"enabled,omitempty"`
	Offset    *float64 `json:"offset,omitempty"`
	Duration  *float64 `json:"duration,omitempty"`
	Easing    string   `json:"easing,omitempty"`
	Immediate bool     `json:"immediate,omitempty"`
	Lock      bool     `json:"lock,omitempty"`
}

type SmoothScrollSnap struct {
	Enabled           bool     `json:"enabled,omitempty"`
	Type              string   `json:"type,omitempty"`
	Lerp              *float64 `json:"lerp,omitempty"`
	Duration          *float64 `json:"duration,omitempty"`
	Easing            string   `json:"easing,omitempty"`
	DistanceThreshold string   `json:"distanceThreshold,omitempty"`
	Debounce          *int     `json:"debounce,omitempty"`
}

var SmoothScrollEasings = []string{
	"easeOutExpo", "linear",
	"easeInQuad", "easeOutQuad", "easeInOutQuad",
	"easeInCubic", "easeOutCubic", "easeInOutCubic",
	"easeInQuart", "easeOutQuart", "easeInOutQuart",
	"easeInExpo", "easeInOutExpo",
	"easeInSine", "easeOutSine", "easeInOutSine",
}

var cubicBezierRe = regexp.MustCompile(`^cubic-bezier\(\s*(-?\d*\.?\d+)\s*,\s*(-?\d*\.?\d+)\s*,\s*(-?\d*\.?\d+)\s*,\s*(-?\d*\.?\d+)\s*\)$`)

func (c *Config) SmoothScrollEnabled() bool {
	return c.SmoothScroll != nil && c.SmoothScroll.Enabled
}

func (s *SmoothScrollConfig) UnmarshalJSON(data []byte) error {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		*s = SmoothScrollConfig{Enabled: b}
		return nil
	}
	type plain SmoothScrollConfig
	p := plain{Enabled: true}
	if err := unmarshalObject(data, &p, "smoothScroll"); err != nil {
		return err
	}
	*s = SmoothScrollConfig(p)
	return nil
}

func (a *SmoothScrollAnchors) UnmarshalJSON(data []byte) error {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		*a = SmoothScrollAnchors{Enabled: b}
		return nil
	}
	type plain SmoothScrollAnchors
	p := plain{Enabled: true}
	if err := unmarshalObject(data, &p, "smoothScroll.anchors"); err != nil {
		return err
	}
	*a = SmoothScrollAnchors(p)
	return nil
}

func (s *SmoothScrollSnap) UnmarshalJSON(data []byte) error {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		*s = SmoothScrollSnap{Enabled: b}
		return nil
	}
	type plain SmoothScrollSnap
	p := plain{Enabled: true}
	if err := unmarshalObject(data, &p, "smoothScroll.snap"); err != nil {
		return err
	}
	*s = SmoothScrollSnap(p)
	return nil
}

func unmarshalObject(data []byte, v any, name string) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s: expected true, false or an object: %w", name, err)
	}
	return nil
}

func validateEasing(name, value string) error {
	if value == "" {
		return nil
	}
	for _, e := range SmoothScrollEasings {
		if e == value {
			return nil
		}
	}
	if m := cubicBezierRe.FindStringSubmatch(value); m != nil {
		x1, err1 := strconv.ParseFloat(m[1], 64)
		x2, err2 := strconv.ParseFloat(m[3], 64)
		if err1 != nil || err2 != nil {
			return fmt.Errorf("%s: invalid cubic-bezier %q", name, value)
		}
		if x1 < 0 || x1 > 1 || x2 < 0 || x2 > 1 {
			return fmt.Errorf("%s: cubic-bezier x values must be between 0 and 1, got %q", name, value)
		}
		return nil
	}
	return fmt.Errorf("%s: unknown easing %q (use one of %s or cubic-bezier(x1,y1,x2,y2))", name, value, strings.Join(SmoothScrollEasings, ", "))
}

func validateUnit(name string, v *float64) error {
	if v != nil && (*v <= 0 || *v > 1) {
		return fmt.Errorf("%s must be greater than 0 and at most 1, got %v", name, *v)
	}
	return nil
}

func validatePositive(name string, v *float64) error {
	if v != nil && *v < 0 {
		return fmt.Errorf("%s must be >= 0, got %v", name, *v)
	}
	return nil
}

func (s *SmoothScrollConfig) Validate() error {
	if s == nil || !s.Enabled {
		return nil
	}
	checks := []error{
		validateUnit("smoothScroll.lerp", s.Lerp),
		validateUnit("smoothScroll.syncTouchLerp", s.SyncTouchLerp),
		validatePositive("smoothScroll.duration", s.Duration),
		validatePositive("smoothScroll.touchMultiplier", s.TouchMultiplier),
		validatePositive("smoothScroll.wheelMultiplier", s.WheelMultiplier),
		validatePositive("smoothScroll.touchInertiaExponent", s.TouchInertiaExponent),
		validateEasing("smoothScroll.easing", s.Easing),
	}
	switch s.Orientation {
	case "", "vertical", "horizontal":
	default:
		checks = append(checks, fmt.Errorf("smoothScroll.orientation must be \"vertical\" or \"horizontal\", got %q", s.Orientation))
	}
	switch s.GestureOrientation {
	case "", "vertical", "horizontal", "both":
	default:
		checks = append(checks, fmt.Errorf("smoothScroll.gestureOrientation must be \"vertical\", \"horizontal\" or \"both\", got %q", s.GestureOrientation))
	}
	if a := s.Anchors; a != nil && a.Enabled {
		checks = append(checks, validatePositive("smoothScroll.anchors.duration", a.Duration), validateEasing("smoothScroll.anchors.easing", a.Easing))
	}
	if sn := s.Snap; sn != nil && sn.Enabled {
		switch sn.Type {
		case "", "proximity", "mandatory", "lock":
		default:
			checks = append(checks, fmt.Errorf("smoothScroll.snap.type must be \"proximity\", \"mandatory\" or \"lock\", got %q", sn.Type))
		}
		checks = append(checks,
			validateUnit("smoothScroll.snap.lerp", sn.Lerp),
			validatePositive("smoothScroll.snap.duration", sn.Duration),
			validateEasing("smoothScroll.snap.easing", sn.Easing))
		if sn.Debounce != nil && *sn.Debounce < 0 {
			checks = append(checks, fmt.Errorf("smoothScroll.snap.debounce must be >= 0, got %d", *sn.Debounce))
		}
		if d := sn.DistanceThreshold; d != "" && !distanceThresholdRe.MatchString(d) {
			checks = append(checks, fmt.Errorf("smoothScroll.snap.distanceThreshold must be a number or a percentage such as \"50%%\", got %q", d))
		}
	}
	for _, err := range checks {
		if err != nil {
			return err
		}
	}
	return nil
}

var distanceThresholdRe = regexp.MustCompile(`^\d+(\.\d+)?%?$`)
