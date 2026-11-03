package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/check"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/migrate"
	"github.com/kratejs/krate/packages/compiler/internal/routetypes"
)

// MigrationVerifyOptions controls a post-migration verification run.
type MigrationVerifyOptions struct {
	// SkipCheck skips the quality gates. Build and route inventory still run.
	SkipCheck bool
}

// MigrationVerification is the evidence produced by VerifyMigration.
type MigrationVerification struct {
	Root               string          `json:"root"`
	BuildOK            bool            `json:"buildOk"`
	BuildError         string          `json:"buildError,omitempty"`
	CheckOK            bool            `json:"checkOk"`
	CheckError         string          `json:"checkError,omitempty"`
	ExpectedRoutes     []string        `json:"expectedRoutes"`
	ActualRoutes       []string        `json:"actualRoutes"`
	MissingRoutes      []string        `json:"missingRoutes,omitempty"`
	ExtraRoutes        []string        `json:"extraRoutes,omitempty"`
	MissingTargets     []string        `json:"missingTargets,omitempty"`
	Findings           []check.Finding `json:"findings,omitempty"`
	FindingsBySeverity map[string]int  `json:"findingsBySeverity,omitempty"`
}

// OK reports whether the migrated app built, passed checks, kept every
// migrated route, and still has every expected target file.
func (v *MigrationVerification) OK() bool {
	return v != nil && v.BuildOK && v.CheckOK && len(v.MissingRoutes) == 0 && len(v.ExtraRoutes) == 0 && len(v.MissingTargets) == 0
}

// VerifyMigration builds a migrated project, runs its quality gates, and diffs
// the built route inventory against the migration plan. It is the reusable
// core behind `krate migrate nextjs --verify` and the reference-app suite.
func VerifyMigration(root string, expected []migrate.Route, opts MigrationVerifyOptions) (*MigrationVerification, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, fmt.Errorf("load migrated config: %w", err)
	}
	ver := &MigrationVerification{Root: root}
	expectedPages := map[string]bool{}
	expectedAll := map[string]bool{}
	for _, route := range expected {
		want := routetypes.Normalize(route.Route)
		if want == "" {
			continue
		}
		expectedAll[want] = true
		if route.Kind == string(migrate.KindAPI) {
			target := filepath.Join(root, filepath.FromSlash(route.Target))
			if _, statErr := os.Stat(target); statErr != nil {
				ver.MissingTargets = append(ver.MissingTargets, route.Target)
			}
			continue
		}
		expectedPages[want] = true
		ver.ExpectedRoutes = append(ver.ExpectedRoutes, want)
	}
	sort.Strings(ver.ExpectedRoutes)
	sort.Strings(ver.MissingTargets)

	builder := New(root, cfg)
	builder.SkipQualityChecks = true
	if err := builder.BuildAll(); err != nil {
		ver.BuildError = err.Error()
		return ver, nil
	}
	ver.BuildOK = true
	routes, err := builder.RouteList()
	if err != nil {
		return nil, fmt.Errorf("list built routes: %w", err)
	}
	actual := map[string]bool{}
	for _, route := range routes {
		want := routetypes.Normalize(route.Route)
		if want == "" {
			continue
		}
		actual[want] = true
		ver.ActualRoutes = append(ver.ActualRoutes, want)
	}
	sort.Strings(ver.ActualRoutes)
	for route := range expectedPages {
		covered := false
		for built := range actual {
			if routeMatches(route, built) {
				covered = true
				break
			}
		}
		if !covered {
			ver.MissingRoutes = append(ver.MissingRoutes, route)
		}
	}
	for route := range actual {
		covered := false
		for expectedRoute := range expectedAll {
			if routeMatches(expectedRoute, route) {
				covered = true
				break
			}
		}
		if !covered {
			ver.ExtraRoutes = append(ver.ExtraRoutes, route)
		}
	}
	sort.Strings(ver.MissingRoutes)
	sort.Strings(ver.ExtraRoutes)

	if opts.SkipCheck {
		ver.CheckOK = true
		return ver, nil
	}
	findings, checkCfg, checkErr := builder.CheckSite(true)
	if checkErr != nil {
		ver.CheckError = checkErr.Error()
		return ver, nil
	}
	ver.Findings = findings
	ver.FindingsBySeverity = map[string]int{}
	for _, finding := range findings {
		ver.FindingsBySeverity[finding.Severity.String()]++
	}
	if check.Failing(findings, checkCfg.FailOn) {
		errs, _ := check.Counts(findings)
		ver.CheckError = fmt.Sprintf("quality checks failed (%d error(s))", errs)
		return ver, nil
	}
	ver.CheckOK = true
	return ver, nil
}

// FormatMigrationVerification prints a compact human-readable verification
// summary. It returns false when the migration did not verify.
func FormatMigrationVerification(ver *MigrationVerification) (string, bool) {
	if ver == nil {
		return "migration verification failed\n", false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "migration verification: %s\n", ver.Root)
	fmt.Fprintf(&b, "  build: %s\n", okLabel(ver.BuildOK, ver.BuildError))
	fmt.Fprintf(&b, "  check: %s\n", okLabel(ver.CheckOK, ver.CheckError))
	fmt.Fprintf(&b, "  routes: %d expected, %d built\n", len(ver.ExpectedRoutes), len(ver.ActualRoutes))
	if len(ver.MissingRoutes) > 0 {
		fmt.Fprintf(&b, "  missing routes: %s\n", strings.Join(ver.MissingRoutes, ", "))
	}
	if len(ver.ExtraRoutes) > 0 {
		fmt.Fprintf(&b, "  extra routes: %s\n", strings.Join(ver.ExtraRoutes, ", "))
	}
	if len(ver.MissingTargets) > 0 {
		fmt.Fprintf(&b, "  missing targets: %s\n", strings.Join(ver.MissingTargets, ", "))
	}
	if len(ver.FindingsBySeverity) > 0 {
		fmt.Fprintf(&b, "  findings:")
		keys := make([]string, 0, len(ver.FindingsBySeverity))
		for key := range ver.FindingsBySeverity {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(&b, " %s=%d", key, ver.FindingsBySeverity[key])
		}
		b.WriteByte('\n')
	}
	return b.String(), ver.OK()
}

func okLabel(ok bool, detail string) string {
	if ok {
		return "ok"
	}
	if detail == "" {
		return "failed"
	}
	return "failed: " + detail
}

// routeMatches reports whether a concrete built route fits an expected route
// pattern. Dynamic segments match one segment; catch-alls match one or more and
// optional catch-alls match zero or more. Concrete generateStaticParams routes
// therefore count as coverage for their [param] page.
func routeMatches(pattern, actual string) bool {
	pattern = routetypes.Normalize(pattern)
	actual = routetypes.Normalize(actual)
	if pattern == actual {
		return true
	}
	if !strings.Contains(pattern, "[") {
		return false
	}
	ps := strings.Split(strings.Trim(pattern, "/"), "/")
	as := strings.Split(strings.Trim(actual, "/"), "/")
	if pattern == "/" {
		return actual == "/"
	}
	for i, seg := range ps {
		if strings.HasPrefix(seg, "[[...") && strings.HasSuffix(seg, "]]") {
			return i <= len(as)
		}
		if strings.HasPrefix(seg, "[...") && strings.HasSuffix(seg, "]") {
			return len(as) > i
		}
		if strings.HasPrefix(seg, "[") && strings.HasSuffix(seg, "]") {
			if i >= len(as) {
				return false
			}
			continue
		}
		if i >= len(as) || seg != as[i] {
			return false
		}
	}
	return len(ps) == len(as)
}
