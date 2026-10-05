// Package check implements compiler-enforced quality gates: static analysis
// rules over a page's AST and its resolved output (final HTML + JS weight).
//
// Built-in rule categories are accessibility (a11y), SEO, and performance
// (perf). Rules run after a build, so they inspect exactly what would ship.
// Findings reuse internal/diag for consistent `file:line:col` formatting, and
// the same rule set backs the `krate check` CLI command.
//
// Custom rules can be authored in TypeScript/JavaScript and run inside the
// embedded QuickJS runtime (see custom.go); no Node is required.
package check

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Severity classifies a finding. Off disables a rule entirely.
type Severity int

const (
	// Off disables the rule (or an individual finding is ignored).
	Off Severity = iota
	// Warning reports a problem without failing the build/check by default.
	Warning
	// Error reports a problem that fails the build/check by default.
	Error
)

func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warning:
		return "warning"
	default:
		return "off"
	}
}

// ParseSeverity parses "error"/"warning"/"off" (case-insensitive). Unknown
// values map to Off.
func ParseSeverity(s string) Severity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "error", "err":
		return Error
	case "warning", "warn":
		return Warning
	default:
		return Off
	}
}

// Rule categories. Also used as the short prefix in a rule ID (`a11y/img-alt`).
const (
	CategoryA11y = "a11y"
	CategorySEO  = "seo"
	CategoryPerf = "perf"
)

// Page is the unit rules are evaluated against: the final shipped HTML plus the
// parsed AST and JS weight for one route.
type Page struct {
	// Route is the URL path (e.g. "/blog/hello"). "/" for the site root.
	Route string
	// RelSource is the source file path relative to the project root.
	RelSource string
	// HTML is the final, fully assembled document (head + body, post-SEO
	// injection, post-minify) — exactly what is written to disk.
	HTML string
	// JSBytes is this route's total client JS weight (page hydration + shared
	// runtime chunk), in bytes.
	JSBytes int
	// Program is the page's parsed program. Optional; may be nil for
	// plugin-generated routes.
	Program *ast.Program
}

// Finding is one quality-gate violation.
type Finding struct {
	Rule     string   `json:"rule"`
	Category string   `json:"category"`
	Severity Severity `json:"severity"`
	Route    string   `json:"route,omitempty"`
	File     string   `json:"file,omitempty"`
	Line     int      `json:"line,omitempty"`
	Col      int      `json:"col,omitempty"`
	Message  string   `json:"message"`
	Hint     string   `json:"hint,omitempty"`
}

// Diagnostic renders the finding through the shared compiler diagnostic type,
// so check output matches parse/build errors.
func (f Finding) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		File:    f.File,
		Line:    f.Line,
		Col:     f.Col,
		Message: f.Message,
		Hint:    f.Hint,
	}
}

func (f Finding) Error() string {
	loc := f.File
	if loc == "" {
		loc = f.Route
	}
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d:%d: %s [%s]", loc, f.Line, f.Col, f.Message, f.Rule)
	}
	return fmt.Sprintf("%s: %s [%s]", loc, f.Message, f.Rule)
}

// builtinRule is a compiled-in rule.
type builtinRule struct {
	id       string
	category string
	severity Severity
	run      func(p *Page, cfg *Config) []Finding
}

// builtinRules is the static rule registry. Ordering only affects output; all
// rules are independent.
var builtinRules = []builtinRule{
	// ── a11y ──────────────────────────────────────────────────────────────
	{"a11y/img-alt", CategoryA11y, Error, ruleImgAlt},
	{"a11y/heading-order", CategoryA11y, Warning, ruleHeadingOrder},
	{"a11y/accessible-name", CategoryA11y, Warning, ruleAccessibleName},
	{"a11y/duplicate-id", CategoryA11y, Warning, ruleDuplicateID},
	{"a11y/landmark", CategoryA11y, Warning, ruleLandmark},
	{"a11y/form-label", CategoryA11y, Warning, ruleFormLabel},
	{"a11y/tabindex", CategoryA11y, Warning, rulePositiveTabindex},
	{"a11y/aria-role", CategoryA11y, Warning, ruleARIARole},
	{"a11y/color-contrast", CategoryA11y, Warning, ruleColorContrast},
	{"a11y/broken-anchor", CategoryA11y, Warning, ruleBrokenAnchor},

	// ── seo ───────────────────────────────────────────────────────────────
	{"seo/title", CategorySEO, Error, ruleTitle},
	{"seo/description", CategorySEO, Warning, ruleDescription},
	{"seo/canonical", CategorySEO, Warning, ruleCanonical},
	{"seo/og", CategorySEO, Warning, ruleOpenGraph},
	{"seo/og-image", CategorySEO, Warning, ruleOGImage},
	{"seo/lang", CategorySEO, Warning, ruleLang},
	{"seo/broken-link", CategorySEO, Warning, ruleBrokenLink},
	{"seo/duplicate-meta", CategorySEO, Warning, ruleDuplicateMeta},

	// ── perf ──────────────────────────────────────────────────────────────
	{"perf/js-budget", CategoryPerf, Warning, ruleJSBudget},
	{"perf/image-dims", CategoryPerf, Warning, ruleImageDims},
}

// KnownRuleIDs returns every built-in rule ID, sorted. Used by docs and
// diagnostics (e.g. listing valid keys for `checks.ignore`).
func KnownRuleIDs() []string {
	ids := make([]string, 0, len(builtinRules))
	for _, r := range builtinRules {
		ids = append(ids, r.id)
	}
	sort.Strings(ids)
	return ids
}

// Run evaluates every enabled rule against every page and returns the combined
// findings, sorted by route then severity (errors first). Custom rules run last.
func Run(cfg Config, pages []Page) ([]Finding, error) {
	if !cfg.Active {
		return nil, nil
	}

	var findings []Finding
	for i := range pages {
		p := &pages[i]
		if p.Route == "" {
			p.Route = "/"
		}
		for _, r := range builtinRules {
			active, override := cfg.rulePolicy(r.id, r.category)
			if !active {
				continue
			}
			for _, f := range r.run(p, &cfg) {
				f.Rule = r.id
				f.Category = r.category
				if override != Off {
					f.Severity = override
				} else if f.Severity == Off {
					f.Severity = r.severity
				}
				if f.Route == "" {
					f.Route = p.Route
				}
				if f.File == "" {
					f.File = p.RelSource
				}
				findings = append(findings, f)
			}
		}
	}

	// Cross-page rules run once over the whole site.
	if active, override := cfg.rulePolicy("seo/duplicate-meta", CategorySEO); active {
		for _, f := range duplicateMetaFindings(pages) {
			f.Rule = "seo/duplicate-meta"
			f.Category = CategorySEO
			if override != Off {
				f.Severity = override
			}
			findings = append(findings, f)
		}
	}

	custom, err := runCustomRules(cfg, pages)
	if err != nil {
		return findings, err
	}
	findings = append(findings, custom...)

	sortFindings(findings)
	return findings, nil
}

// sortFindings orders findings for stable, readable output: by route, then
// descending severity, then rule ID, then message.
func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.Route != b.Route {
			return a.Route < b.Route
		}
		if a.Severity != b.Severity {
			return a.Severity > b.Severity
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Message < b.Message
	})
}

// Counts returns the number of error and warning findings.
func Counts(fs []Finding) (errors, warnings int) {
	for _, f := range fs {
		switch f.Severity {
		case Error:
			errors++
		case Warning:
			warnings++
		}
	}
	return
}

// Failing reports whether any finding meets or exceeds the configured failure
// threshold. Off never fails.
func Failing(fs []Finding, failOn Severity) bool {
	if failOn == Off {
		return false
	}
	for _, f := range fs {
		if f.Severity >= failOn {
			return true
		}
	}
	return false
}
