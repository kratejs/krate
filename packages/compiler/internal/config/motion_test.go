package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodeMotion(t *testing.T, raw string) *MotionConfig {
	t.Helper()
	var m MotionConfig
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return &m
}

func TestMotionBoolForms(t *testing.T) {
	if m := decodeMotion(t, `true`); !m.Enabled {
		t.Fatal("true must enable motion")
	}
	if m := decodeMotion(t, `false`); m.Enabled {
		t.Fatal("false must leave motion off")
	}
	if m := decodeMotion(t, `{"reducedMotion":"never"}`); !m.Enabled {
		t.Fatal("an object enables motion unless enabled is false")
	}
	if m := decodeMotion(t, `{"enabled":false}`); m.Enabled {
		t.Fatal("enabled:false must win over the object form")
	}
}

func TestMotionRejectsWrongType(t *testing.T) {
	var m MotionConfig
	if err := json.Unmarshal([]byte(`"yes"`), &m); err == nil || !strings.Contains(err.Error(), "motion") {
		t.Fatalf("expected a motion type error, got %v", err)
	}
}

func TestMotionEnabledAndDeclarative(t *testing.T) {
	c := Default()
	if c.MotionEnabled() {
		t.Fatal("motion must be off by default")
	}
	c.Motion = decodeMotion(t, `true`)
	if !c.MotionEnabled() || !c.Motion.DeclarativeEnabled() {
		t.Fatal("true enables motion and declarative attributes")
	}
	c.Motion = decodeMotion(t, `{"declarative":false}`)
	if c.Motion.DeclarativeEnabled() {
		t.Fatal("declarative:false must disable data-motion binding")
	}
}

func TestMotionValidateAccepts(t *testing.T) {
	m := decodeMotion(t, `{
		"reducedMotion": "user",
		"transition": {"type":"spring","stiffness":200,"damping":20,"mass":1,"bounce":0.25,"ease":"snappy","repeat":2,"repeatType":"mirror"},
		"viewport": {"amount":0.3,"margin":"0px 0px -10%","once":true},
		"stagger": {"delay":0.08,"from":"center","ease":"easeOut"},
		"easings": {"snappy":[0.2,0.9,0.3,1]},
		"presets": {"pop":{"initial":{"scale":0.8,"opacity":0},"animate":{"scale":[0.8,1.05,1],"opacity":1},"transition":{"duration":0.4,"ease":"cubic-bezier(0.2,0,0,1)"}}}
	}`)
	if err := m.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	var nilCfg *MotionConfig
	if err := nilCfg.Validate(); err != nil {
		t.Fatalf("nil config must validate: %v", err)
	}
	if err := decodeMotion(t, `{"enabled":false,"reducedMotion":"nonsense"}`).Validate(); err != nil {
		t.Fatalf("a disabled config is not validated: %v", err)
	}
}

func TestMotionValidateRejects(t *testing.T) {
	cases := map[string]struct{ raw, want string }{
		"reduced":         {`{"reducedMotion":"sometimes"}`, "reducedMotion"},
		"type":            {`{"transition":{"type":"bouncy"}}`, "transition.type"},
		"negative":        {`{"transition":{"duration":-1}}`, "transition.duration"},
		"stiffness":       {`{"transition":{"stiffness":0}}`, "transition.stiffness"},
		"bounce":          {`{"transition":{"bounce":2}}`, "transition.bounce"},
		"repeat type":     {`{"transition":{"repeatType":"zigzag"}}`, "repeatType"},
		"ease":            {`{"transition":{"ease":"wobble"}}`, "unknown easing"},
		"bezier x":        {`{"transition":{"ease":"cubic-bezier(2,0,0,1)"}}`, "x values"},
		"amount word":     {`{"viewport":{"amount":"half"}}`, "viewport.amount"},
		"amount range":    {`{"viewport":{"amount":1.5}}`, "viewport.amount"},
		"margin":          {`{"viewport":{"margin":"big"}}`, "viewport.margin"},
		"stagger delay":   {`{"stagger":{"delay":-0.1}}`, "stagger.delay"},
		"stagger from":    {`{"stagger":{"from":"middle"}}`, "stagger.from"},
		"easing builtin":  {`{"easings":{"easeIn":[0,0,1,1]}}`, "built-in"},
		"easing points":   {`{"easings":{"snappy":[0,0,1]}}`, "four numbers"},
		"easing x":        {`{"easings":{"snappy":[2,0,1,1]}}`, "between 0 and 1"},
		"preset name":     {`{"presets":{"Bad Name":{"animate":{"opacity":1}}}}`, "preset name"},
		"preset empty":    {`{"presets":{"nothing":{}}}`, "at least one"},
		"preset prop":     {`{"presets":{"p":{"animate":{"bad-prop":1}}}}`, "property name"},
		"preset value":    {`{"presets":{"p":{"animate":{"opacity":true}}}}`, "expected a string"},
		"preset empty kf": {`{"presets":{"p":{"animate":{"opacity":[]}}}}`, "cannot be empty"},
		"preset ease":     {`{"presets":{"p":{"animate":{"opacity":1},"transition":{"ease":"nope"}}}}`, "unknown easing"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := decodeMotion(t, tc.raw).Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestMotionCustomEasingUsableInPreset(t *testing.T) {
	m := decodeMotion(t, `{"easings":{"snappy":[0.2,0.9,0.3,1]},"presets":{"p":{"animate":{"opacity":1},"transition":{"ease":"snappy"}}}}`)
	if err := m.Validate(); err != nil {
		t.Fatalf("custom easing must be accepted in presets: %v", err)
	}
}

func TestMotionConfigValidateThroughConfig(t *testing.T) {
	c := Default()
	c.Motion = decodeMotion(t, `{"reducedMotion":"bad"}`)
	if _, err := c.Validate(); err == nil {
		t.Fatal("Config.Validate must surface motion errors")
	}
}

func TestMotionApplyConfigProp(t *testing.T) {
	c := Default()
	if err := applyConfigProp(c, "motion", map[string]interface{}{"reducedMotion": "always", "viewport": map[string]interface{}{"once": true}}); err != nil {
		t.Fatal(err)
	}
	if c.Motion == nil || !c.Motion.Enabled || c.Motion.ReducedMotion != "always" || c.Motion.Viewport == nil || c.Motion.Viewport.Once == nil || !*c.Motion.Viewport.Once {
		t.Fatalf("motion not applied: %+v", c.Motion)
	}
	c = Default()
	if err := applyConfigProp(c, "motion", true); err != nil || !c.MotionEnabled() {
		t.Fatalf("motion:true not applied: %v", err)
	}
}

func TestMotionKnownKeys(t *testing.T) {
	if !knownTopLevelKeys["motion"] {
		t.Fatal("motion must be a known top-level key")
	}
	for _, k := range []string{"enabled", "reducedMotion", "declarative", "transition", "viewport", "stagger", "easings", "presets"} {
		if !knownNestedKeys["motion"][k] {
			t.Errorf("knownNestedKeys[motion] missing %q", k)
		}
	}
	w := UnknownKeyWarnings([]byte(`{"motion":{"reducedMotoin":"user"}}`))
	if !strings.Contains(strings.Join(w, "\n"), "motion.reducedMotoin") {
		t.Fatalf("expected a typo warning, got %v", w)
	}
}
