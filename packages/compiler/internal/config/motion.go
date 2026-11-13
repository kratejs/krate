package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type MotionConfig struct {
	Enabled       bool                    `json:"enabled,omitempty"`
	ReducedMotion string                  `json:"reducedMotion,omitempty"`
	Declarative   *bool                   `json:"declarative,omitempty"`
	Transition    *MotionTransition       `json:"transition,omitempty"`
	Viewport      *MotionViewport         `json:"viewport,omitempty"`
	Stagger       *MotionStagger          `json:"stagger,omitempty"`
	Easings       map[string][]float64    `json:"easings,omitempty"`
	Presets       map[string]MotionPreset `json:"presets,omitempty"`
}

type MotionTransition struct {
	Type           string   `json:"type,omitempty"`
	Duration       *float64 `json:"duration,omitempty"`
	Delay          *float64 `json:"delay,omitempty"`
	Ease           string   `json:"ease,omitempty"`
	Stiffness      *float64 `json:"stiffness,omitempty"`
	Damping        *float64 `json:"damping,omitempty"`
	Mass           *float64 `json:"mass,omitempty"`
	Bounce         *float64 `json:"bounce,omitempty"`
	VisualDuration *float64 `json:"visualDuration,omitempty"`
	Repeat         *int     `json:"repeat,omitempty"`
	RepeatType     string   `json:"repeatType,omitempty"`
	RepeatDelay    *float64 `json:"repeatDelay,omitempty"`
}

type MotionViewport struct {
	Amount json.RawMessage `json:"amount,omitempty"`
	Margin string          `json:"margin,omitempty"`
	Once   *bool           `json:"once,omitempty"`
}

type MotionStagger struct {
	Delay *float64        `json:"delay,omitempty"`
	From  json.RawMessage `json:"from,omitempty"`
	Ease  string          `json:"ease,omitempty"`
}

type MotionPreset struct {
	Initial    map[string]any    `json:"initial,omitempty"`
	Animate    map[string]any    `json:"animate,omitempty"`
	Hover      map[string]any    `json:"hover,omitempty"`
	Press      map[string]any    `json:"press,omitempty"`
	Transition *MotionTransition `json:"transition,omitempty"`
}

var MotionEasings = []string{
	"linear", "easeIn", "easeOut", "easeInOut",
	"circIn", "circOut", "circInOut",
	"backIn", "backOut", "backInOut",
	"anticipate",
}

var MotionTransitionTypes = []string{"spring", "tween", "inertia"}

var MotionBuiltinPresets = []string{
	"fade", "fade-up", "fade-down", "fade-left", "fade-right",
	"zoom-in", "zoom-out", "blur-in", "slide-up", "slide-down", "slide-left", "slide-right",
	"flip-x", "flip-y", "rotate-in",
}

var (
	motionNameRe   = regexp.MustCompile(`^[a-z][a-zA-Z0-9-]*$`)
	motionMarginRe = regexp.MustCompile(`^-?\d+(\.\d+)?(px|%)?(\s+-?\d+(\.\d+)?(px|%)?){0,3}$`)
	motionBezierRe = regexp.MustCompile(`^cubic-bezier\(\s*(-?\d*\.?\d+)\s*,\s*(-?\d*\.?\d+)\s*,\s*(-?\d*\.?\d+)\s*,\s*(-?\d*\.?\d+)\s*\)$`)
	motionKeyRe    = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*$`)
)

func (c *Config) MotionEnabled() bool {
	return c.Motion != nil && c.Motion.Enabled
}

func (m *MotionConfig) DeclarativeEnabled() bool {
	return m == nil || m.Declarative == nil || *m.Declarative
}

func (m *MotionConfig) UnmarshalJSON(data []byte) error {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		*m = MotionConfig{Enabled: b}
		return nil
	}
	type plain MotionConfig
	p := plain{Enabled: true}
	if err := decodeMotionObject(data, &p, "motion"); err != nil {
		return err
	}
	*m = MotionConfig(p)
	return nil
}

func decodeMotionObject(data []byte, v any, name string) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s: expected true, false or an object: %w", name, err)
	}
	return nil
}

func (m *MotionConfig) Validate() error {
	if m == nil || !m.Enabled {
		return nil
	}
	switch m.ReducedMotion {
	case "", "user", "always", "never":
	default:
		return fmt.Errorf("motion.reducedMotion must be \"user\", \"always\" or \"never\", got %q", m.ReducedMotion)
	}
	for _, name := range sortedKeys(m.Easings) {
		if !motionNameRe.MatchString(name) {
			return fmt.Errorf("motion.easings: %q is not a valid easing name", name)
		}
		for _, b := range MotionEasings {
			if b == name {
				return fmt.Errorf("motion.easings: %q is a built-in easing and cannot be redefined", name)
			}
		}
		pts := m.Easings[name]
		if len(pts) != 4 {
			return fmt.Errorf("motion.easings.%s must be four numbers [x1, y1, x2, y2], got %d", name, len(pts))
		}
		if pts[0] < 0 || pts[0] > 1 || pts[2] < 0 || pts[2] > 1 {
			return fmt.Errorf("motion.easings.%s: x1 and x2 must be between 0 and 1", name)
		}
	}
	if err := m.Transition.validate("motion.transition", m.Easings); err != nil {
		return err
	}
	if err := m.Viewport.validate(); err != nil {
		return err
	}
	if err := m.Stagger.validate(m.Easings); err != nil {
		return err
	}
	for _, name := range sortedKeys(m.Presets) {
		if !motionNameRe.MatchString(name) {
			return fmt.Errorf("motion.presets: %q is not a valid preset name (use letters, digits and hyphens, starting with a letter)", name)
		}
		if err := m.Presets[name].validate("motion.presets."+name, m.Easings); err != nil {
			return err
		}
	}
	return nil
}

func (t *MotionTransition) validate(name string, custom map[string][]float64) error {
	if t == nil {
		return nil
	}
	if t.Type != "" && !containsString(MotionTransitionTypes, t.Type) {
		return fmt.Errorf("%s.type must be one of %s, got %q", name, strings.Join(MotionTransitionTypes, ", "), t.Type)
	}
	for _, f := range []struct {
		n string
		v *float64
	}{
		{"duration", t.Duration}, {"delay", t.Delay}, {"visualDuration", t.VisualDuration},
		{"repeatDelay", t.RepeatDelay},
	} {
		if f.v != nil && *f.v < 0 {
			return fmt.Errorf("%s.%s must be >= 0, got %v", name, f.n, *f.v)
		}
	}
	for _, f := range []struct {
		n string
		v *float64
	}{
		{"stiffness", t.Stiffness}, {"damping", t.Damping}, {"mass", t.Mass},
	} {
		if f.v != nil && *f.v <= 0 {
			return fmt.Errorf("%s.%s must be greater than 0, got %v", name, f.n, *f.v)
		}
	}
	if t.Bounce != nil && (*t.Bounce < 0 || *t.Bounce > 1) {
		return fmt.Errorf("%s.bounce must be between 0 and 1, got %v", name, *t.Bounce)
	}
	if t.Repeat != nil && *t.Repeat < 0 {
		return fmt.Errorf("%s.repeat must be >= 0, got %d", name, *t.Repeat)
	}
	switch t.RepeatType {
	case "", "loop", "reverse", "mirror":
	default:
		return fmt.Errorf("%s.repeatType must be \"loop\", \"reverse\" or \"mirror\", got %q", name, t.RepeatType)
	}
	return validateMotionEase(name+".ease", t.Ease, custom)
}

func validateMotionEase(name, value string, custom map[string][]float64) error {
	if value == "" || containsString(MotionEasings, value) {
		return nil
	}
	if _, ok := custom[value]; ok {
		return nil
	}
	if m := motionBezierRe.FindStringSubmatch(value); m != nil {
		x1, _ := strconv.ParseFloat(m[1], 64)
		x2, _ := strconv.ParseFloat(m[3], 64)
		if x1 < 0 || x1 > 1 || x2 < 0 || x2 > 1 {
			return fmt.Errorf("%s: cubic-bezier x values must be between 0 and 1, got %q", name, value)
		}
		return nil
	}
	return fmt.Errorf("%s: unknown easing %q (use one of %s, a name from motion.easings, or cubic-bezier(x1,y1,x2,y2))", name, value, strings.Join(MotionEasings, ", "))
}

func (v *MotionViewport) validate() error {
	if v == nil {
		return nil
	}
	if len(v.Amount) > 0 {
		var s string
		var n float64
		switch {
		case json.Unmarshal(v.Amount, &s) == nil:
			if s != "some" && s != "all" {
				return fmt.Errorf("motion.viewport.amount must be \"some\", \"all\" or a number between 0 and 1, got %q", s)
			}
		case json.Unmarshal(v.Amount, &n) == nil:
			if n < 0 || n > 1 {
				return fmt.Errorf("motion.viewport.amount must be between 0 and 1, got %v", n)
			}
		default:
			return fmt.Errorf("motion.viewport.amount must be \"some\", \"all\" or a number between 0 and 1")
		}
	}
	if v.Margin != "" && !motionMarginRe.MatchString(strings.TrimSpace(v.Margin)) {
		return fmt.Errorf("motion.viewport.margin must be one to four lengths in px or %%, such as \"0px 0px -10%%\", got %q", v.Margin)
	}
	return nil
}

func (s *MotionStagger) validate(custom map[string][]float64) error {
	if s == nil {
		return nil
	}
	if s.Delay != nil && *s.Delay < 0 {
		return fmt.Errorf("motion.stagger.delay must be >= 0, got %v", *s.Delay)
	}
	if len(s.From) > 0 {
		var str string
		var n int
		switch {
		case json.Unmarshal(s.From, &str) == nil:
			if str != "first" && str != "last" && str != "center" {
				return fmt.Errorf("motion.stagger.from must be \"first\", \"last\", \"center\" or an index, got %q", str)
			}
		case json.Unmarshal(s.From, &n) == nil:
			if n < 0 {
				return fmt.Errorf("motion.stagger.from must be >= 0, got %d", n)
			}
		default:
			return fmt.Errorf("motion.stagger.from must be \"first\", \"last\", \"center\" or an index")
		}
	}
	return validateMotionEase("motion.stagger.ease", s.Ease, custom)
}

func (p MotionPreset) validate(name string, custom map[string][]float64) error {
	if len(p.Initial) == 0 && len(p.Animate) == 0 && len(p.Hover) == 0 && len(p.Press) == 0 {
		return fmt.Errorf("%s must define at least one of initial, animate, hover or press", name)
	}
	for field, target := range map[string]map[string]any{
		"initial": p.Initial, "animate": p.Animate, "hover": p.Hover, "press": p.Press,
	} {
		for _, k := range sortedKeys(target) {
			if !motionKeyRe.MatchString(k) && !strings.HasPrefix(k, "--") {
				return fmt.Errorf("%s.%s: %q is not a valid property name (use camelCase such as \"backgroundColor\" or a CSS variable)", name, field, k)
			}
			if err := validateMotionValue(fmt.Sprintf("%s.%s.%s", name, field, k), target[k]); err != nil {
				return err
			}
		}
	}
	return p.Transition.validate(name+".transition", custom)
}

func validateMotionValue(name string, v any) error {
	switch x := v.(type) {
	case string, float64, nil:
		return nil
	case []any:
		if len(x) == 0 {
			return fmt.Errorf("%s: a keyframe list cannot be empty", name)
		}
		for _, e := range x {
			switch e.(type) {
			case string, float64, nil:
			default:
				return fmt.Errorf("%s: keyframes must be strings, numbers or null", name)
			}
		}
		return nil
	default:
		return fmt.Errorf("%s: expected a string, a number or a list of keyframes", name)
	}
}

func containsString(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
