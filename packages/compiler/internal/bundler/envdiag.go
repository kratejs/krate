package bundler

import (
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// PublicEnvPrefix names the environment variables that are inlined into client
// code and static output.
const PublicEnvPrefix = "KRATE_PUBLIC_"

var envReadRe = regexp.MustCompile(`(import\.meta\.env|process\.env)(?:\.([A-Za-z_$][A-Za-z0-9_$]*)|\[\s*["']([^"']+)["']\s*\])?`)

// envReadDiags reports environment reads in client or statically rendered code
// that are not defined at compile time (see Options.Define). They would
// otherwise render empty with no explanation.
func (b *Bundler) envReadDiags(path, src string, class ComponentClass) []diag.Diagnostic {
	if class != ComponentClassClient && class != ComponentClassStatic {
		return nil
	}
	if !strings.Contains(src, "import.meta.env") && !strings.Contains(src, "process.env") {
		return nil
	}
	// A module with server actions legitimately reads server environment.
	if strings.Contains(src, "use server") {
		return nil
	}
	var out []diag.Diagnostic
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "*") || strings.HasPrefix(trim, "/*") {
			continue
		}
		for _, m := range envReadRe.FindAllStringSubmatchIndex(line, -1) {
			root := line[m[2]:m[3]]
			name := ""
			if m[4] >= 0 {
				name = line[m[4]:m[5]]
			} else if m[6] >= 0 {
				name = line[m[6]:m[7]]
			}
			if name != "" {
				if _, ok := b.tsOpts.Define[root+"."+name]; ok {
					continue
				}
			}
			what := root
			if name != "" {
				what = root + "." + name
			}
			col := len([]rune(line[:m[0]])) + 1
			out = append(out, diag.Diagnostic{
				File: path, Line: i + 1, Col: col, Source: strings.TrimRight(line, "\r"),
				Severity: diag.SeverityWarning,
				Message:  what + " is not exposed to the browser or to statically rendered output; it reads as undefined",
				Hint:     "name the variable with the " + PublicEnvPrefix + " prefix (for example " + PublicEnvPrefix + "API_URL) to inline it at build time, or read it in a server component, API route or server action",
			}.WithCode(diag.CodeEnvNotExposed))
		}
	}
	return out
}
