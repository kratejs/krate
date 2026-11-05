package build

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/syntaxhighlight"
)

// Code whose text changes at run time is highlighted in the browser. The
// tokenizer is one shared chunk and each language is another (its lexer tables
// and the names that select it), both written only for the languages a page
// uses. A page's script maps a language to its chunk (window.__KRATE_HL) and
// the "highlight" runtime feature loads it on first use, so pages without
// such code ship none of it.

const (
	highlightMarkup = "data-krate-hl"
	highlightAPI    = "krateHighlight("
)

var highlightMarkupRe = regexp.MustCompile(highlightMarkup + `="([^"]*)"`)

type highlightChunks struct {
	mu     sync.Mutex
	core   string            // chunks/hl.<hash>.js, once written
	langs  map[string]string // lexer name -> chunk path relative to outDir
	warned map[string]bool
}

// highlightCore writes the shared tokenizer chunk and returns its path
// relative to the output directory ("" when the runtime package is missing).
func (b *Builder) highlightCore() string {
	hc := &b.hl
	if hc.core != "" {
		return hc.core
	}
	src := loadNamedBundleFromDisk(b.Root, "highlight.js")
	if src == "" {
		return ""
	}
	res := api.Transform(src, api.TransformOptions{
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		Format:            api.FormatESModule,
		Target:            api.ES2020,
		Charset:           api.CharsetUTF8,
		Loader:            api.LoaderJS,
	})
	if len(res.Errors) > 0 {
		return ""
	}
	code := strings.TrimSpace(string(res.Code))
	rel := "chunks/hl." + hashContent([]byte(code)) + ".js"
	if err := os.MkdirAll(filepath.Join(b.Cfg.OutDir, "chunks"), 0o755); err != nil {
		return ""
	}
	_ = fsutil.WriteFileIfChanged(filepath.Join(b.Cfg.OutDir, rel), []byte(code), 0o644)
	hc.core = rel
	return rel
}

// highlightLangChunk writes the chunk for a lexer (and the lexers it calls
// into) and returns its path relative to the output directory.
func (b *Builder) highlightLangChunk(lexer string) (string, error) {
	hc := &b.hl
	if rel, ok := hc.langs[lexer]; ok {
		return rel, nil
	}
	core := b.highlightCore()
	if core == "" {
		return "", fmt.Errorf("the highlighter (highlight.js) was not found next to the hydration runtime; rebuild @krate/runtime")
	}
	bundle, err := syntaxhighlight.BuildBundle([]string{lexer})
	if err != nil {
		return "", err
	}
	names := map[string][]string{}
	for k, v := range bundle.Keys {
		names[v] = append(names[v], k)
	}
	var sb strings.Builder
	sb.WriteString(`import{registerLanguage as r`)
	if bundle.Fences != nil {
		sb.WriteString(`,setFenceResolver as f`)
	}
	sb.WriteString(`}from"./` + strings.TrimPrefix(core, "chunks/") + `";`)
	for _, t := range bundle.Tables {
		keys := names[strings.ToLower(t.Name)]
		sort.Strings(keys)
		sb.WriteString("r(")
		sb.Write(t.JSON())
		sb.WriteString(",")
		sb.Write(jsonStrings(keys))
		sb.WriteString(");")
	}
	if bundle.Fences != nil {
		var unloaded []string
		for n, target := range bundle.Fences {
			if target == "" {
				unloaded = append(unloaded, n)
			}
		}
		sort.Strings(unloaded)
		sb.WriteString(`{const s=new Set(`)
		sb.WriteString(hlJSString(strings.Join(unloaded, "\n")))
		sb.WriteString(`.split("\n"));f(n=>s.has(n.trim().toLowerCase()))}`)
	}
	sb.WriteString(`export*from"./` + strings.TrimPrefix(core, "chunks/") + `";`)
	code := sb.String()
	rel := "chunks/hl-" + sanitizeChunkName(lexer) + "." + hashContent([]byte(code)) + ".js"
	_ = fsutil.WriteFileIfChanged(filepath.Join(b.Cfg.OutDir, rel), []byte(code), 0o644)
	if hc.langs == nil {
		hc.langs = map[string]string{}
	}
	hc.langs[lexer] = rel
	return rel, nil
}

func sanitizeChunkName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+':
			b.WriteString("p")
		case r == '#':
			b.WriteString("s")
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func jsonStrings(s []string) []byte {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, v := range s {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(hlJSString(v))
	}
	sb.WriteByte(']')
	return []byte(sb.String())
}

// hlJSString writes a double-quoted JavaScript string.
func hlJSString(v string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range v {
		switch r {
		case '"', '\\':
			sb.WriteByte('\\')
			sb.WriteRune(r)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case ' ', ' ', '<':
			fmt.Fprintf(&sb, `\u%04x`, r)
		default:
			if r < 0x20 {
				fmt.Fprintf(&sb, `\u%04x`, r)
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// attachHighlight appends what a page needs to highlight code in the browser
// to its script: the language-to-chunk map and, when the page has dynamic
// code, the call that starts it. It returns js unchanged for pages without
// either.
func (b *Builder) attachHighlight(js, html string) string {
	hasCode := strings.Contains(html, highlightMarkup)
	if !hasCode && !strings.Contains(js, highlightAPI) {
		return js
	}
	langs := map[string]bool{}
	for _, m := range highlightMarkupRe.FindAllStringSubmatch(html, -1) {
		if m[1] != "" {
			langs[m[1]] = true
		}
	}
	for _, l := range b.Cfg.Highlight.Languages {
		langs[l] = true
	}
	b.hl.mu.Lock()
	defer b.hl.mu.Unlock()
	names := make([]string, 0, len(langs))
	for l := range langs {
		names = append(names, l)
	}
	sort.Strings(names)
	base := b.Cfg.BaseURLPath()
	var entries []string
	for _, lang := range names {
		lexer, ok := syntaxhighlight.ShippedLexer(lang)
		if !ok {
			b.warnHighlightLanguage(lang)
			continue
		}
		rel, err := b.highlightLangChunk(lexer)
		if err != nil {
			b.warnHighlightChunk(lang, err)
			continue
		}
		entries = append(entries, hlJSString(lang)+":"+hlJSString(base+"/"+rel))
	}
	if len(entries) == 0 {
		return js
	}
	snippet := "window.__KRATE_HL=Object.assign(window.__KRATE_HL||{},{" + strings.Join(entries, ",") + "});"
	if hasCode {
		snippet += "if(typeof krateHighlightInit==='function')krateHighlightInit();"
	}
	if strings.TrimSpace(js) == "" {
		return snippet
	}
	return js + "\n" + snippet
}

func (b *Builder) warnHighlightLanguage(lang string) {
	if b.hl.warned == nil {
		b.hl.warned = map[string]bool{}
	}
	if b.hl.warned[lang] {
		return
	}
	b.hl.warned[lang] = true
	printWarningsOnce(os.Stderr, []diag.Diagnostic{{
		Severity: diag.SeverityWarning,
		Code:     diag.CodeHighlightLanguageUnavailable,
		Message:  fmt.Sprintf("code in %q cannot be highlighted in the browser: the runtime tokenizer does not ship that language, so it is shown as plain text", lang),
		Hint:     "use one of " + strings.Join(syntaxhighlight.ShippedLanguages, ", ") + ", or make the code known at build time so the compiler highlights it",
	}})
}

func (b *Builder) warnHighlightChunk(lang string, err error) {
	printWarningsOnce(os.Stderr, []diag.Diagnostic{{
		Severity: diag.SeverityWarning,
		Code:     diag.CodeHighlightLanguageUnavailable,
		Message:  fmt.Sprintf("code in %q cannot be highlighted in the browser: %v", lang, err),
		Hint:     "rebuild @krate/runtime so highlight.js exists next to the hydration runtime",
	}})
}

// serverLangRe finds the literal language of a <Code> or <SyntaxHighlight> in
// a compiled server bundle (`lang: "ts"`).
var serverLangRe = regexp.MustCompile(`\blang\s*:\s*["']([A-Za-z0-9_+#.-]{1,24})["']`)

// writeHighlightTables writes the lexer tables the Node renderer uses to
// highlight code that only exists at request time: the languages named in
// highlight.languages and the literal languages of the server pages. The
// renderer loads dist/.krate/highlight.json at startup; without it, such code
// is written as plain text.
func (b *Builder) writeHighlightTables(serverBundles map[string]string) {
	path := filepath.Join(b.Cfg.OutDir, ".krate", "highlight.json")
	if len(serverBundles) == 0 {
		_ = os.Remove(path)
		return
	}
	lexers := map[string]bool{}
	add := func(lang string) {
		if lexer, ok := syntaxhighlight.ShippedLexer(lang); ok {
			lexers[lexer] = true
		}
	}
	for _, l := range b.Cfg.Highlight.Languages {
		add(l)
	}
	for _, rel := range serverBundles {
		data, err := os.ReadFile(filepath.Join(b.Cfg.OutDir, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		for _, m := range serverLangRe.FindAllStringSubmatch(string(data), -1) {
			add(m[1])
		}
	}
	if len(lexers) == 0 {
		_ = os.Remove(path)
		return
	}
	names := make([]string, 0, len(lexers))
	for l := range lexers {
		names = append(names, l)
	}
	sort.Strings(names)
	bundle, err := syntaxhighlight.BuildBundle(names)
	if err != nil {
		b.warnHighlightChunk(strings.Join(names, ", "), err)
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = fsutil.WriteFileAtomic(path, bundle.JSON(), 0o644)
}
