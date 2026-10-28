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
	fontLocalImport  = regexp.MustCompile(`import\s+([A-Za-z_$][\w$]*)\s+from\s+["']next/font/local["']`)
)

// migrateNextFonts rewrites next/font imports to a generated local helper and
// stylesheet. Google fonts load from the Google Fonts CSS endpoint; local font
// files still need manual @font-face setup.
func migrateNextFonts(rep *Report) {
	fonts := map[string]bool{}
	local := map[string]bool{}
	for _, f := range rep.Files {
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
		for _, m := range fontLocalImport.FindAllStringSubmatch(src, -1) {
			local[m[1]] = true
		}
	}
	if len(fonts) == 0 && len(local) == 0 {
		return
	}

	var css strings.Builder
	css.WriteString("/* Google fonts loaded by the migration helper. */\n")
	names := make([]string, 0, len(fonts))
	for name := range fonts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		slug := fontSlug(name)
		fmt.Fprintf(&css, `@import url("https://fonts.googleapis.com/css2?family=%s&display=swap");
.krate-font-%s { font-family: "%s", ui-sans-serif, system-ui, sans-serif; }
.krate-font-%s-variable { --font-%s: "%s"; }
`, name, slug, name, slug, slug, name)
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
	if len(local) > 0 {
		helper.WriteString("\nexport default function localFont(options: any) {\n")
		helper.WriteString("  const family = options?.family || \"LocalFont\";\n")
		helper.WriteString("  return __krateFont(family)(options);\n")
		helper.WriteString("}\n")
	}

	for i := range rep.Files {
		f := &rep.Files[i]
		src := string(f.Content)
		if !strings.Contains(src, "next/font/") {
			continue
		}
		rel := relativeModulePath(f.Target, nextFontHelper)
		src = replaceModule(src, "next/font/google", rel)
		src = replaceModule(src, "next/font/local", rel)
		if src != string(f.Content) {
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
	rep.Files = append(rep.Files,
		PlannedFile{Kind: KindAsset, Target: nextFontHelper, Content: []byte(helper.String()), Rewrites: []string{"generated next/font helper"}},
		PlannedFile{Kind: KindAsset, Target: nextFontCSS, Content: []byte(css.String()), Rewrites: []string{"generated Google font CSS"}},
	)
	rep.Warnings = append(rep.Warnings, "next/font uses the Google Fonts CSS endpoint; local font files need manual @font-face setup")
	if len(local) > 0 {
		rep.Manual = append(rep.Manual, ManualItem{Source: "next/font/local", Reason: "local font files are not copied or self-hosted", Hint: "add @font-face rules and point the helper at the files"})
	}
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
