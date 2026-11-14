package migrate

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	nextFontHelper = "src/migrated/next-font.ts"
	nextFontCSS    = "src/migrated/next-fonts.css"
)

var (
	fontGoogleImport = regexp.MustCompile(`import\s+(?:type\s+)?\{([^}]*)\}\s+from\s+["']next/font/google["']`)
	fontLocalImport  = regexp.MustCompile(`import\s+([A-Za-z_$][\w$]*)\s+from\s+["']next/font/local["']\s*;?[ \t]*\r?\n?`)
)

// migrateNextFonts rewrites next/font imports. Google fonts go through a
// generated helper and the Google Fonts CSS endpoint. next/font/local calls and
// geist/font/{sans,mono} become generated @font-face CSS plus the font files
// copied into public/, with className/variable/style objects in place of the
// calls.
func migrateNextFonts(rep *Report, root string) {
	lf := newLocalFonts(root, rep)
	fonts := map[string]bool{}
	n := len(rep.Files)
	for i := 0; i < n; i++ {
		f := &rep.Files[i]
		if f.Kind == KindPublic || len(f.Content) == 0 {
			continue
		}
		src := string(f.Content)
		for _, m := range fontGoogleImport.FindAllStringSubmatch(src, -1) {
			for _, item := range strings.Split(m[1], ",") {
				name := strings.TrimSpace(item)
				if i := strings.Index(name, " as "); i >= 0 {
					name = strings.TrimSpace(name[:i])
				}
				if name != "" {
					fonts[name] = true
				}
			}
		}
		if out, changed := lf.rewrite(f, src); changed {
			out = insertImportAfterDirective(out, "import \""+relativeModulePath(f.Target, nextFontCSS)+"\";\n")
			f.Content = []byte(out)
			f.Dependencies = append(f.Dependencies, nextFontCSS)
		}
	}
	rep.Files = append(rep.Files, lf.pending...)
	if len(fonts) == 0 && !lf.used() {
		return
	}

	var css strings.Builder
	names := make([]string, 0, len(fonts))
	for name := range fonts {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > 0 {
		css.WriteString("/* Google fonts loaded by the migration helper. */\n")
	}
	for _, name := range names {
		slug := fontSlug(name)
		fmt.Fprintf(&css, `@import url("https://fonts.googleapis.com/css2?family=%s&display=swap");
.krate-font-%s { font-family: "%s", ui-sans-serif, system-ui, sans-serif; }
.krate-font-%s-variable { --font-%s: "%s"; }
`, name, slug, name, slug, slug, name)
	}
	if lf.used() {
		if len(names) > 0 {
			css.WriteString("\n")
		}
		css.WriteString("/* Local and geist fonts, self-hosted from public/" + publicFontDir + ". */\n")
		css.WriteString(lf.css.String())
	}

	var helper strings.Builder
	helper.WriteString("import \"./next-fonts.css\";\n\n")
	helper.WriteString("function __krateFont(family: string): any {\n")
	helper.WriteString("  const slug = family.toLowerCase().replace(/[^a-z0-9]+/g, \"-\").replace(/^-|-$/g, \"\");\n")
	helper.WriteString("  return (_options?: any) => ({\n")
	helper.WriteString("    className: \"krate-font-\" + slug,\n")
	helper.WriteString("    variable: \"krate-font-\" + slug + \"-variable\",\n")
	helper.WriteString("    style: { fontFamily: family },\n")
	helper.WriteString("  });\n")
	helper.WriteString("}\n\n")
	for _, name := range names {
		fmt.Fprintf(&helper, "export const %s = __krateFont(%q);\n", name, name)
	}

	if len(names) > 0 {
		for i := range rep.Files {
			f := &rep.Files[i]
			src := string(f.Content)
			if !strings.Contains(src, "next/font/google") {
				continue
			}
			rel := relativeModulePath(f.Target, nextFontHelper)
			src = replaceModule(src, "next/font/google", rel)
			f.Content = []byte(src)
			f.Rewrites = append(f.Rewrites, "rewrote next/font imports to generated helper")
			f.Dependencies = append(f.Dependencies, nextFontHelper, nextFontCSS)
		}
	}
	for _, f := range rep.Files {
		if f.Target == nextFontHelper {
			rep.Warnings = append(rep.Warnings, "next-font helper already planned; next/font imports use it")
			return
		}
	}
	rep.Files = append(rep.Files, PlannedFile{Kind: KindAsset, Target: nextFontCSS, Content: []byte(css.String()), Rewrites: []string{"generated font CSS"}})
	if len(names) > 0 {
		rep.Files = append(rep.Files, PlannedFile{Kind: KindAsset, Target: nextFontHelper, Content: []byte(helper.String()), Rewrites: []string{"generated next/font helper"}})
		rep.Warnings = append(rep.Warnings, "next/font/google uses the Google Fonts CSS endpoint; self-host the font files if the site must not call Google")
	}
}

// insertImportAfterDirective adds line to the top of src, below any directive
// prologue ("use client", "use server", "use strict"), so a directive stays the
// first statement.
func insertImportAfterDirective(src, line string) string {
	at := directivePrologueEnd(src)
	return src[:at] + line + src[at:]
}

// directivePrologueEnd returns the offset just past the last directive line at
// the top of src, skipping comments and blank lines before and between them.
// It returns 0 when src has no directive.
func directivePrologueEnd(src string) int {
	end, i := 0, 0
	for i < len(src) {
		switch {
		case src[i] == ' ' || src[i] == '\t' || src[i] == '\r' || src[i] == '\n':
			i++
		case strings.HasPrefix(src[i:], "//"):
			n := strings.IndexByte(src[i:], '\n')
			if n < 0 {
				return end
			}
			i += n + 1
		case strings.HasPrefix(src[i:], "/*"):
			n := strings.Index(src[i+2:], "*/")
			if n < 0 {
				return end
			}
			i += n + 4
		case src[i] == '"' || src[i] == '\'':
			q := src[i]
			j := i + 1
			for j < len(src) && src[j] != q && src[j] != '\n' && src[j] != '\\' {
				j++
			}
			if j >= len(src) || src[j] != q {
				return end
			}
			j++
			for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
				j++
			}
			if j < len(src) && src[j] == ';' {
				j++
			}
			for j < len(src) && (src[j] == ' ' || src[j] == '\t' || src[j] == '\r') {
				j++
			}
			if j < len(src) && src[j] != '\n' {
				return end
			}
			if j < len(src) {
				j++
			}
			end, i = j, j
		default:
			return end
		}
	}
	return end
}

func fontSlug(name string) string {
	s := strings.ToLower(name)
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
