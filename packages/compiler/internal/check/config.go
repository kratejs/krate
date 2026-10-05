package check

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// Config controls which built-in rules run and how findings are reported. It is
// built from the `checks` key in krate.config.ts (see FromMap) or from defaults
// for `krate check` when no config is present.
type Config struct {
	// Active is false when checks are disabled entirely.
	Active bool
	// Categories maps a category name ("a11y"/"seo"/"perf") to its severity.
	// Absent categories use the built-in default (all on).
	Categories map[string]Severity
	// Rules maps a full rule ID ("a11y/img-alt") to a severity override.
	Rules map[string]Severity
	// Ignore is the set of rule IDs to suppress.
	Ignore map[string]bool
	// JSBudgetBytes is the per-route client JS budget. 0 disables the rule.
	JSBudgetBytes int
	// FailOn is the minimum severity that makes `krate check` (and a checks-
	// enabled build) exit non-zero.
	FailOn Severity
	// Custom lists paths (relative to Root) to JS/TS modules exporting a
	// `check` rule (see custom.go).
	Custom []string

	// Root and Env are carried to custom (plugin) rules.
	Root string
	Env  map[string]string

	// Routes is the set of known site routes (leading slash; stored both with
	// and without a trailing slash) used by the broken-link rule. Populated by
	// the build pipeline before Run; empty disables the rule.
	Routes map[string]bool
}

// customModules resolves configured custom rule paths against Root.
func (c *Config) customModules() []string {
	if len(c.Custom) == 0 {
		return nil
	}
	out := make([]string, 0, len(c.Custom))
	for _, m := range c.Custom {
		if m == "" {
			continue
		}
		if filepath.IsAbs(m) {
			out = append(out, m)
			continue
		}
		out = append(out, filepath.Join(c.Root, m))
	}
	return out
}

// DefaultConfig returns an active config with every category enabled and
// failOn=error. Used by `krate check` when no `checks` config is present.
func DefaultConfig() Config {
	return Config{
		Active:     true,
		Categories: map[string]Severity{},
		Rules:      map[string]Severity{},
		Ignore:     map[string]bool{},
		FailOn:     Error,
	}
}

// Disabled returns an inactive config.
func Disabled() Config { return Config{} }

// rulePolicy resolves whether a rule runs and any severity override. It applies,
// in order: ignore list, per-rule override, then category override.
func (c *Config) rulePolicy(id, category string) (active bool, override Severity) {
	if c.Ignore[id] {
		return false, Off
	}
	if sev, ok := c.Rules[id]; ok {
		if sev == Off {
			return false, Off
		}
		return true, sev
	}
	if sev, ok := c.Categories[category]; ok {
		if sev == Off {
			return false, Off
		}
		return true, sev
	}
	return true, Off
}

// FromMap builds a Config from the raw `checks` object parsed out of
// krate.config.ts. Unknown keys are ignored for forward compatibility.
//
//	checks: {
//	  a11y: "error" | "warning" | false,   // category toggle/severity
//	  seo: true,
//	  perf: true,
//	  rules: { "a11y/img-alt": "off" },
//	  ignore: ["seo/canonical"],
//	  budget: { js: 100 },                  // KB per route
//	  failOn: "error" | "warning",
//	}
func FromMap(m map[string]any, root string, env map[string]string) (Config, error) {
	// A nil/absent `checks` key leaves the gates inactive: compiler-enforced
	// checks are strictly opt-in for builds. `krate check` supplies DefaultConfig
	// when it wants to run the built-ins regardless.
	cfg := DefaultConfig()
	cfg.Active = m != nil
	cfg.Root = root
	cfg.Env = env
	if m == nil {
		return cfg, nil
	}

	// `enabled: false` (or `checks: false`) turns everything off.
	if v, ok := m["enabled"]; ok {
		if b, ok := v.(bool); ok && !b {
			return Config{}, nil
		}
	}

	for _, cat := range []string{CategoryA11y, CategorySEO, CategoryPerf} {
		v, ok := m[cat]
		if !ok {
			continue
		}
		// `true` means "enabled with the built-in default severity" — leave the
		// category absent so rulePolicy falls through to each rule's default.
		if b, isBool := v.(bool); isBool {
			if b {
				continue
			}
			cfg.Categories[cat] = Off
			continue
		}
		sev, err := severityValue(v)
		if err != nil {
			return cfg, fmt.Errorf("checks.%s: %w", cat, err)
		}
		cfg.Categories[cat] = sev
	}

	if raw, ok := m["rules"]; ok {
		rules, ok := raw.(map[string]any)
		if !ok {
			return cfg, fmt.Errorf("checks.rules: expected object, got %T", raw)
		}
		for id, v := range rules {
			if b, isBool := v.(bool); isBool {
				if b {
					delete(cfg.Rules, id)
					continue
				}
				cfg.Rules[id] = Off
				continue
			}
			sev, err := severityValue(v)
			if err != nil {
				return cfg, fmt.Errorf("checks.rules.%s: %w", id, err)
			}
			cfg.Rules[id] = sev
		}
	}

	if raw, ok := m["ignore"]; ok {
		arr, ok := raw.([]any)
		if !ok {
			return cfg, fmt.Errorf("checks.ignore: expected array, got %T", raw)
		}
		for _, item := range arr {
			if s, ok := item.(string); ok {
				cfg.Ignore[s] = true
			}
		}
	}

	if raw, ok := m["budget"]; ok {
		budget, ok := raw.(map[string]any)
		if !ok {
			return cfg, fmt.Errorf("checks.budget: expected object, got %T", raw)
		}
		if v, ok := budget["js"]; ok {
			kb, err := numberValue(v)
			if err != nil {
				return cfg, fmt.Errorf("checks.budget.js: %w", err)
			}
			cfg.JSBudgetBytes = int(kb * 1024)
		}
	}

	if v, ok := m["failOn"]; ok {
		s, ok := v.(string)
		if !ok {
			return cfg, fmt.Errorf("checks.failOn: expected string, got %T", v)
		}
		cfg.FailOn = ParseSeverity(s)
		if cfg.FailOn == Off {
			cfg.FailOn = Error
		}
	}

	if raw, ok := m["custom"]; ok {
		arr, ok := raw.([]any)
		if !ok {
			return cfg, fmt.Errorf("checks.custom: expected array, got %T", raw)
		}
		for _, item := range arr {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				cfg.Custom = append(cfg.Custom, s)
			}
		}
	}

	return cfg, nil
}

// severityValue interprets a config value as a severity. Strings map through
// ParseSeverity. Booleans are handled by callers before reaching here.
func severityValue(v any) (Severity, error) {
	switch t := v.(type) {
	case string:
		sev := ParseSeverity(t)
		if sev == Off && !strings.EqualFold(strings.TrimSpace(t), "off") {
			return Off, fmt.Errorf("unknown severity %q (expected error|warning|off)", t)
		}
		return sev, nil
	default:
		return Off, fmt.Errorf("expected severity string, got %T", v)
	}
}

func numberValue(v any) (float64, error) {
	switch n := v.(type) {
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case float64:
		return n, nil
	case json.Number:
		f, err := n.Float64()
		return f, err
	default:
		return 0, fmt.Errorf("expected number, got %T", v)
	}
}
