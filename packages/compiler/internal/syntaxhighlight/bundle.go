package syntaxhighlight

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/alecthomas/chroma/v2/lexers"
)

// A Bundle is a set of exported lexers plus the language names that select
// them, ready to ship to the tokenizer.
type Bundle struct {
	Tables []*Table
	// Keys maps a lower-case language name to the lower-case name of its table.
	Keys map[string]string
	// Fences holds the names chroma would resolve to some lexer, for bundles
	// that include a lexer choosing sublexers by name (markdown). Every name
	// maps to the table that handles it, or "" when that lexer is not in the
	// bundle.
	Fences map[string]string
}

// BuildBundle exports the named languages and everything they call into.
func BuildBundle(langs []string) (*Bundle, error) {
	b := &Bundle{Keys: map[string]string{}}
	seen := map[string]bool{}
	var add func(name string) error
	add = func(name string) error {
		lex := lexers.Get(name)
		if lex == nil {
			return fmt.Errorf("no chroma lexer %q", name)
		}
		canon := lex.Config().Name
		if seen[canon] {
			return nil
		}
		seen[canon] = true
		t, err := ExportLexer(canon)
		if err != nil {
			return err
		}
		b.Tables = append(b.Tables, t)
		for _, d := range t.Deps {
			if err := add(d); err != nil {
				return err
			}
		}
		if t.Frontmatter {
			if err := add("yaml"); err != nil {
				return err
			}
		}
		return nil
	}
	for _, l := range langs {
		if err := add(NormalizeLanguage(l)); err != nil {
			return nil, err
		}
	}
	sort.Slice(b.Tables, func(i, j int) bool { return b.Tables[i].Name < b.Tables[j].Name })
	for _, t := range b.Tables {
		for _, k := range languageKeys(t.Name) {
			b.Keys[k] = strings.ToLower(t.Name)
		}
	}
	for _, t := range b.Tables {
		if t.Fences {
			b.Fences = fenceNames(b)
			break
		}
	}
	return b, nil
}

// languageKeys lists every language string whose highlighting resolves to the
// named lexer: its name and aliases, its file extensions, and the short names
// the compiler normalises to it.
func languageKeys(name string) []string {
	lex := lexers.Get(name)
	cfg := lex.Config()
	cand := map[string]bool{strings.ToLower(cfg.Name): true}
	for _, a := range cfg.Aliases {
		cand[strings.ToLower(a)] = true
	}
	for _, g := range append(append([]string{}, cfg.Filenames...), cfg.AliasFilenames...) {
		if strings.HasPrefix(g, "*.") && !strings.ContainsAny(g[2:], "*?[") {
			cand[strings.ToLower(g[2:])] = true
		}
	}
	for k := range languageAliases {
		cand[k] = true
	}
	var out []string
	for k := range cand {
		got := lexers.Get(NormalizeLanguage(k))
		if got != nil && got.Config().Name == cfg.Name {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// fenceNames lists names a fenced code block may use that chroma resolves:
// every lexer name and alias, plain file extensions and exact file names.
func fenceNames(b *Bundle) map[string]string {
	loaded := map[string]string{}
	for _, t := range b.Tables {
		loaded[t.Name] = strings.ToLower(t.Name)
	}
	out := map[string]string{}
	for _, l := range lexers.GlobalLexerRegistry.Lexers {
		cfg := l.Config()
		target := loaded[cfg.Name]
		names := []string{cfg.Name}
		names = append(names, cfg.Aliases...)
		for _, g := range append(append([]string{}, cfg.Filenames...), cfg.AliasFilenames...) {
			if strings.HasPrefix(g, "*.") && !strings.ContainsAny(g[2:], "*?[") {
				names = append(names, g[2:])
			} else if !strings.ContainsAny(g, "*?[") {
				names = append(names, g)
			}
		}
		for _, n := range names {
			k := strings.ToLower(n)
			if prev, ok := out[k]; ok && prev != "" && target == "" {
				continue
			}
			out[k] = target
		}
	}
	// Keep only names that Registry.Get resolves, with the lexer it picks.
	for k := range out {
		got := lexers.Get(k)
		if got == nil {
			delete(out, k)
			continue
		}
		out[k] = loaded[got.Config().Name]
	}
	return out
}

// JSON encodes the bundle: {"tables":[...],"keys":{...}}.
func (b *Bundle) JSON() []byte {
	var buf bytes.Buffer
	buf.WriteString(`{"tables":[`)
	for i, t := range b.Tables {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(t.JSON())
	}
	buf.WriteString(`],"keys":`)
	buf.Write(jsonNoEscape(b.Keys))
	if b.Fences != nil {
		buf.WriteString(`,"fences":`)
		buf.Write(jsonNoEscape(b.Fences))
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

func jsonNoEscape(v interface{}) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimRight(buf.Bytes(), "\n")
}
