package build

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
)

// revalidatePathCallRe matches revalidatePath("literal") and
// revalidatePath("literal", "type") calls with a string-literal path.
var revalidatePathCallRe = regexp.MustCompile("revalidatePath\\(\\s*([\"'`])(/[^\"'`$]*)([\"'`])\\s*(?:,\\s*[\"'`](page|layout)[\"'`]\\s*)?\\)")

var revalidateScanExts = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".mts": true}

var revalidateScanSkip = map[string]bool{"node_modules": true, "dist": true, ".krate": true, ".git": true}

// staticRevalidateTargetWarnings scans the project's sources for
// revalidatePath calls with a literal path and warns (KR8603) for each one
// whose target page is static (SSG): at runtime such a call can only report
// the route as skipped.
func staticRevalidateTargetWarnings(srcDir, root string, pages []PageMeta) []diag.Diagnostic {
	if len(pages) == 0 {
		return nil
	}
	var out []diag.Diagnostic
	_ = fsutil.WalkExt(srcDir, revalidateScanExts, revalidateScanSkip, func(path string, _ os.FileInfo) error {
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), "revalidatePath(") {
			return nil
		}
		src := string(data)
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		for _, m := range revalidatePathCallRe.FindAllStringSubmatchIndex(src, -1) {
			if src[m[2]:m[3]] != src[m[6]:m[7]] {
				continue
			}
			target := src[m[4]:m[5]]
			typ := "page"
			if m[8] >= 0 {
				typ = src[m[8]:m[9]]
			}
			targets, _, skipped, err := resolveRevalidatePath(pages, revalidatePathReq{Path: target, Type: typ})
			if err != nil || len(skipped) == 0 {
				continue
			}
			var routes []string
			for _, s := range skipped {
				routes = append(routes, s.Target)
			}
			sort.Strings(routes)
			pos := offsetPos(src, m[0])
			msg := "revalidatePath(\"" + target + "\") targets static (SSG) page " + strings.Join(routes, ", ") + "; its HTML is baked at build time, so the call can only report it as skipped"
			if len(targets) > 0 {
				msg = "revalidatePath(\"" + target + "\", \"" + typ + "\") also covers static (SSG) pages " + strings.Join(routes, ", ") + "; they are baked at build time and are reported as skipped"
			}
			hint := "make the target page ISR (`export const revalidate = N`) or SSR (`export const dynamic = \"force-dynamic\"`)"
			out = append(out, warnAt(filepath.ToSlash(rel), src, pos, diag.CodeRevalidateStaticTarget, msg, hint))
		}
		return nil
	})
	return out
}

// offsetPos converts a byte offset to a 1-based line/column.
func offsetPos(src string, off int) ast.Pos {
	line := 1 + strings.Count(src[:off], "\n")
	col := off - strings.LastIndex(src[:off], "\n")
	return ast.Pos{Line: line, Col: col, Offset: off}
}
