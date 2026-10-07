package config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

type configParser struct {
	tokens []lexer.Token
	pos    int
}

func (p *configParser) peek() lexer.Token {
	if p.pos >= len(p.tokens) {
		return lexer.Token{Kind: lexer.EOF}
	}
	return p.tokens[p.pos]
}

func (p *configParser) advance() lexer.Token {
	t := p.peek()
	p.pos++
	return t
}

func (p *configParser) skip(kind lexer.Kind) bool {
	if p.peek().Kind == kind {
		p.advance()
		return true
	}
	return false
}

func parseTSConfig(src string, cfg *Config) error {
	raw := lexer.New(src).Tokenize()
	var toks []lexer.Token
	for _, t := range raw {
		if t.Kind != lexer.Whitespace {
			toks = append(toks, t)
		}
	}
	p := &configParser{tokens: toks}
	return p.parseRoot(cfg)
}

func (p *configParser) parseRoot(cfg *Config) error {
	if err := p.expect(lexer.Export, "export"); err != nil {
		return err
	}
	if err := p.expect(lexer.Default_, "default"); err != nil {
		return err
	}
	obj, err := p.parseObject()
	if err != nil {
		return err
	}
	recordUnknownKeys(obj)
	for _, prop := range obj {
		if err := applyConfigProp(cfg, prop.key, prop.val); err != nil {
			return fmt.Errorf("property %q: %w", prop.key, err)
		}
	}
	return nil
}

// recordUnknownKeys appends a warning for each unrecognized top-level or nested
// key. Called after a successful static parse so typos surface, mirroring the
// JS-executed path (UnknownKeyWarnings).
func recordUnknownKeys(obj []configProp) {
	m := make(map[string]interface{}, len(obj))
	for _, prop := range obj {
		m[prop.key] = prop.val
	}
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	Warnings = append(Warnings, UnknownKeyWarnings(data)...)
}

type configProp struct {
	key string
	val interface{}
}

func (p *configParser) parseObject() ([]configProp, error) {
	if err := p.expect(lexer.LBRACE, "{"); err != nil {
		return nil, err
	}
	var props []configProp
	for p.peek().Kind != lexer.RBRACE && p.peek().Kind != lexer.EOF {
		if len(props) > 0 {
			p.skip(lexer.COMMA)
		}
		if p.peek().Kind == lexer.RBRACE || p.peek().Kind == lexer.EOF {
			break
		}
		key, err := p.parseKey()
		if err != nil {
			return nil, err
		}
		if err := p.expect(lexer.COLON, ":"); err != nil {
			return nil, err
		}
		val, err := p.parseValue()
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", key, err)
		}
		props = append(props, configProp{key, val})
	}
	if err := p.expect(lexer.RBRACE, "}"); err != nil {
		return nil, err
	}
	return props, nil
}

func (p *configParser) parseKey() (string, error) {
	t := p.advance()
	if t.Kind == lexer.String {
		return t.Value, nil
	}
	// Accept identifiers AND reserved words used as property names (e.g.
	// `type`, `default`, `class`). Any token whose value is a valid identifier
	// is a valid unquoted object key in JS/TS.
	if isIdentifierName(t.Value) {
		return t.Value, nil
	}
	return "", fmt.Errorf("expected property key (identifier or string), got %q at line %d", t.Value, t.Line)
}

// isIdentifierName reports whether s is a valid JS identifier (ASCII subset),
// used to accept reserved words as object keys.
func isIdentifierName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_' || r == '$':
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

func (p *configParser) parseValue() (interface{}, error) {
	t := p.peek()
	switch t.Kind {
	case lexer.String:
		p.advance()
		s := t.Value
		if len(s) >= 2 {
			s = s[1 : len(s)-1]
		}
		return s, nil
	case lexer.Number:
		p.advance()
		return parseNumber(t.Value)
	case lexer.True:
		p.advance()
		return true, nil
	case lexer.False:
		p.advance()
		return false, nil
	case lexer.Null_:
		p.advance()
		return nil, nil
	case lexer.LBRACE:
		props, err := p.parseObject()
		if err != nil {
			return nil, err
		}
		m := make(map[string]interface{}, len(props))
		for _, prop := range props {
			m[prop.key] = prop.val
		}
		return m, nil
	case lexer.LBRACKET:
		return p.parseArray()
	default:
		return nil, fmt.Errorf("unexpected token %q at line %d", t.Value, t.Line)
	}
}

func (p *configParser) parseArray() ([]interface{}, error) {
	p.advance()
	var items []interface{}
	for p.peek().Kind != lexer.RBRACKET && p.peek().Kind != lexer.EOF {
		if len(items) > 0 {
			p.skip(lexer.COMMA)
		}
		if p.peek().Kind == lexer.RBRACKET || p.peek().Kind == lexer.EOF {
			break
		}
		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		items = append(items, val)
	}
	if err := p.expect(lexer.RBRACKET, "]"); err != nil {
		return nil, err
	}
	return items, nil
}

func (p *configParser) expect(kind lexer.Kind, desc string) error {
	t := p.advance()
	if t.Kind != kind {
		return fmt.Errorf("expected %s, got %q at line %d", desc, t.Value, t.Line)
	}
	return nil
}

func parseNumber(s string) (interface{}, error) {
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i, nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, nil
	}
	return nil, fmt.Errorf("invalid number %q", s)
}

// configInt coerces a parsed numeric config value to int.
func configInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

// configInt64 coerces a parsed numeric config value to int64.
func configInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	}
	return 0, false
}

// configStrings appends the string items of a parsed array to dst.
func configStrings(dst []string, v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return dst
	}
	for _, item := range arr {
		if s, ok := item.(string); ok {
			dst = append(dst, s)
		}
	}
	return dst
}

// parseSidecar builds a SidecarConfig from a statically parsed object.
func parseSidecar(v interface{}) (*SidecarConfig, error) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("expected object, got %T", v)
	}
	sc := &SidecarConfig{}
	for k, raw := range m {
		switch k {
		case "command":
			if s, ok := raw.(string); ok {
				sc.Command = s
			}
		case "args":
			sc.Args = configStrings(sc.Args, raw)
		case "cwd":
			if s, ok := raw.(string); ok {
				sc.Cwd = s
			}
		case "env":
			if em, ok := raw.(map[string]interface{}); ok {
				sc.Env = make(map[string]string, len(em))
				for ek, ev := range em {
					if es, ok := ev.(string); ok {
						sc.Env[ek] = es
					}
				}
			}
		case "port":
			if n, ok := configInt(raw); ok {
				sc.Port = n
			}
		case "target":
			if s, ok := raw.(string); ok {
				sc.Target = s
			}
		case "prefix":
			if s, ok := raw.(string); ok {
				sc.Prefix = s
			}
		}
	}
	return sc, nil
}

// interfaceStrings converts a parsed JSON array of values to []string.
func interfaceStrings(arr []interface{}) []string {
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func applyConfigProp(cfg *Config, key string, val interface{}) error {
	switch key {
	case "entry":
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", val)
		}
		cfg.Entry = s
	case "outDir":
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", val)
		}
		cfg.OutDir = s
	case "pagesDir":
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", val)
		}
		cfg.PagesDir = s
	case "publicDir":
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", val)
		}
		cfg.PublicDir = s
	case "minify":
		b, ok := val.(bool)
		if !ok {
			return fmt.Errorf("expected boolean, got %T", val)
		}
		cfg.Minify = b
	case "minifyHTML":
		b, ok := val.(bool)
		if !ok {
			return fmt.Errorf("expected boolean, got %T", val)
		}
		cfg.MinifyHTML = b
	case "minifyCSS":
		b, ok := val.(bool)
		if !ok {
			return fmt.Errorf("expected boolean, got %T", val)
		}
		cfg.MinifyCSS = b
	case "minifyJS":
		b, ok := val.(bool)
		if !ok {
			return fmt.Errorf("expected boolean, got %T", val)
		}
		cfg.MinifyJS = b
	case "sourcemap":
		b, ok := val.(bool)
		if !ok {
			return fmt.Errorf("expected boolean, got %T", val)
		}
		cfg.Sourcemap = b
	case "basePath":
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", val)
		}
		cfg.BasePath = s
	case "viewTransitions":
		switch v := val.(type) {
		case bool:
			if v {
				cfg.ViewTransitions = "auto"
			} else {
				cfg.ViewTransitions = "off"
			}
		case string:
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "", "auto", "on", "true":
				cfg.ViewTransitions = "auto"
			case "off", "false", "none":
				cfg.ViewTransitions = "off"
			default:
				return fmt.Errorf("viewTransitions: expected \"auto\" or \"off\", got %q", v)
			}
		default:
			return fmt.Errorf("viewTransitions: expected boolean or string, got %T", val)
		}
	case "ppr":
		switch v := val.(type) {
		case bool:
			cfg.PPR = v
		case map[string]interface{}:
			cfg.PPR = true
			if n, ok := v["revalidate"]; ok {
				switch rv := n.(type) {
				case int64:
					cfg.PPRRevalidate = int(rv)
				case float64:
					cfg.PPRRevalidate = int(rv)
				default:
					return fmt.Errorf("ppr.revalidate: expected number, got %T", n)
				}
			}
		default:
			return fmt.Errorf("ppr: expected boolean or object, got %T", val)
		}
	case "server":
		m, ok := val.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected object, got %T", val)
		}
		for k, v := range m {
			switch k {
			case "host":
				if s, ok := v.(string); ok {
					cfg.Server.Host = s
				}
			case "port":
				if n, ok := configInt(v); ok {
					cfg.Server.Port = n
				}
			case "maxBodySize":
				if n, ok := configInt64(v); ok {
					cfg.Server.MaxBodySize = n
				}
			}
		}
	case "cors":
		m, ok := val.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected object, got %T", val)
		}
		for k, v := range m {
			switch k {
			case "enabled":
				if b, ok := v.(bool); ok {
					cfg.CORS.Enabled = b
				}
			case "origins":
				cfg.CORS.Origins = configStrings(cfg.CORS.Origins, v)
			case "methods":
				cfg.CORS.Methods = configStrings(cfg.CORS.Methods, v)
			case "headers":
				cfg.CORS.Headers = configStrings(cfg.CORS.Headers, v)
			case "credentials":
				if b, ok := v.(bool); ok {
					cfg.CORS.Credentials = b
				}
			case "maxAge":
				if n, ok := configInt(v); ok {
					cfg.CORS.MaxAge = n
				}
			}
		}
	case "api":
		m, ok := val.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected object, got %T", val)
		}
		if sc, ok := m["sidecar"]; ok && sc != nil {
			parsed, err := parseSidecar(sc)
			if err != nil {
				return fmt.Errorf("api.sidecar: %w", err)
			}
			cfg.API.Sidecar = parsed
		}
	case "devServer":
		m, ok := val.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected object, got %T", val)
		}
		for k, v := range m {
			switch k {
			case "port":
				switch n := v.(type) {
				case int64:
					cfg.DevServer.Port = int(n)
				case float64:
					cfg.DevServer.Port = int(n)
				default:
					return fmt.Errorf("devServer.port: expected number, got %T", v)
				}
			case "open":
				b, ok := v.(bool)
				if !ok {
					return fmt.Errorf("devServer.open: expected boolean, got %T", v)
				}
				cfg.DevServer.Open = b
			case "overlay":
				b, ok := v.(bool)
				if !ok {
					return fmt.Errorf("devServer.overlay: expected boolean, got %T", v)
				}
				cfg.DevServer.Overlay = &b
			case "toolbar":
				b, ok := v.(bool)
				if !ok {
					return fmt.Errorf("devServer.toolbar: expected boolean, got %T", v)
				}
				cfg.DevServer.Toolbar = &b
			case "editor":
				s, ok := v.(string)
				if !ok {
					return fmt.Errorf("devServer.editor: expected string, got %T", v)
				}
				cfg.DevServer.Editor = s
			}
		}
	case "emitReact":
		// Deprecated no-op: React-to-krate transpilation is always enabled.
		// Accepted silently so existing configs keep loading without warnings.
	case "markdown":
		m, ok := val.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected object, got %T", val)
		}
		for k, v := range m {
			switch k {
			case "gfm":
				if b, ok := v.(bool); ok {
					cfg.Markdown.GFM = b
				}
			case "headingAnchors":
				if b, ok := v.(bool); ok {
					cfg.Markdown.HeadingAnchors = b
				}
			case "admonitions":
				if b, ok := v.(bool); ok {
					cfg.Markdown.Admonitions = b
				}
			case "codeHighlight":
				if b, ok := v.(bool); ok {
					cfg.Markdown.CodeHighlight = b
				}
			case "codeTheme":
				if s, ok := v.(string); ok {
					cfg.Markdown.CodeTheme = s
				}
			case "math":
				if b, ok := v.(bool); ok {
					cfg.Markdown.Math = b
				}
			case "mermaid":
				if b, ok := v.(bool); ok {
					cfg.Markdown.Mermaid = b
				}
			}
		}
	case "tailwind":
		m, ok := val.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected object, got %T", val)
		}
		for k, v := range m {
			switch k {
			case "enabled":
				if b, ok := v.(bool); ok {
					cfg.Tailwind.Enabled = b
				}
			case "scanDirs":
				arr, ok := v.([]interface{})
				if !ok {
					return fmt.Errorf("tailwind.scanDirs: expected array, got %T", v)
				}
				for _, item := range arr {
					if s, ok := item.(string); ok {
						cfg.Tailwind.ScanDirs = append(cfg.Tailwind.ScanDirs, s)
					}
				}
			case "content":
				switch cv := v.(type) {
				case []interface{}:
					for _, item := range cv {
						if s, ok := item.(string); ok {
							cfg.Tailwind.Content = append(cfg.Tailwind.Content, s)
						}
					}
				case map[string]interface{}:
					if files, ok := cv["files"].([]interface{}); ok {
						for _, item := range files {
							if s, ok := item.(string); ok {
								cfg.Tailwind.Content = append(cfg.Tailwind.Content, s)
							}
						}
					}
				}
			case "preflight":
				if b, ok := v.(bool); ok {
					cfg.Tailwind.Preflight = b
				}
			case "strict":
				if b, ok := v.(bool); ok {
					cfg.Tailwind.Strict = b
				}
			case "darkMode":
				if s, ok := v.(string); ok {
					cfg.Tailwind.DarkMode = s
				}
			case "executeConfig":
				if b, ok := v.(bool); ok {
					cfg.Tailwind.ExecuteConfig = b
				}
			}
		}
	case "csp":
		m, ok := val.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected object, got %T", val)
		}
		for k, v := range m {
			switch k {
			case "enabled":
				if b, ok := v.(bool); ok {
					cfg.CSP.Enabled = b
				}
			case "directive":
				if s, ok := v.(string); ok {
					cfg.CSP.Directive = s
				}
			}
		}
	case "ssr":
		m, ok := val.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected object, got %T", val)
		}
		for k, v := range m {
			switch k {
			case "rendererPort":
				switch n := v.(type) {
				case int64:
					cfg.SSR.RendererPort = int(n)
				case float64:
					cfg.SSR.RendererPort = int(n)
				}
			case "timeout":
				switch n := v.(type) {
				case int64:
					cfg.SSR.Timeout = int(n)
				case float64:
					cfg.SSR.Timeout = int(n)
				}
			case "maxCacheSize":
				switch n := v.(type) {
				case int64:
					cfg.SSR.MaxCacheSize = int(n)
				case float64:
					cfg.SSR.MaxCacheSize = int(n)
				}
			case "middlewareRuntime":
				if s, ok := v.(string); ok {
					cfg.SSR.MiddlewareRuntime = s
				}
			case "apiRuntime":
				if s, ok := v.(string); ok {
					cfg.SSR.APIRuntime = s
				}
			case "ssrRuntime":
				if s, ok := v.(string); ok {
					cfg.SSR.SSRRuntime = s
				}
			case "streaming":
				if b, ok := v.(bool); ok {
					cfg.SSR.Streaming = b
				}
			}
		}
	case "plugins":
		arr, ok := val.([]interface{})
		if !ok {
			return fmt.Errorf("expected array, got %T", val)
		}
		for i, item := range arr {
			m, ok := item.(map[string]interface{})
			if !ok {
				return fmt.Errorf("plugins[%d]: expected object, got %T", i, item)
			}
			pc := PluginConfig{}
			if name, ok := m["name"]; ok {
				if s, ok := name.(string); ok {
					pc.Name = s
				} else {
					return fmt.Errorf("plugins[%d].name: expected string, got %T", i, name)
				}
			}
			if mod, ok := m["module"]; ok {
				if s, ok := mod.(string); ok {
					pc.Module = s
				} else {
					return fmt.Errorf("plugins[%d].module: expected string, got %T", i, mod)
				}
			}
			if ord, ok := m["order"]; ok {
				if f, ok := ord.(float64); ok {
					pc.Order = int(f)
				} else {
					return fmt.Errorf("plugins[%d].order: expected number, got %T", i, ord)
				}
			}
			if opts, ok := m["options"]; ok {
				if m2, ok := opts.(map[string]interface{}); ok {
					pc.Options = m2
				} else {
					return fmt.Errorf("plugins[%d].options: expected object, got %T", i, opts)
				}
			}
			cfg.Plugins = append(cfg.Plugins, pc)
		}
	case "redirects":
		arr, ok := val.([]interface{})
		if !ok {
			return fmt.Errorf("expected array, got %T", val)
		}
		for i, item := range arr {
			m, ok := item.(map[string]interface{})
			if !ok {
				return fmt.Errorf("redirects[%d]: expected object, got %T", i, item)
			}
			rd := Redirect{}
			if s, ok := m["source"].(string); ok {
				rd.Source = s
			}
			if s, ok := m["destination"].(string); ok {
				rd.Destination = s
			}
			if b, ok := m["permanent"].(bool); ok {
				rd.Permanent = b
			}
			cfg.Redirects = append(cfg.Redirects, rd)
		}
	case "rewrites":
		arr, ok := val.([]interface{})
		if !ok {
			return fmt.Errorf("expected array, got %T", val)
		}
		for i, item := range arr {
			m, ok := item.(map[string]interface{})
			if !ok {
				return fmt.Errorf("rewrites[%d]: expected object, got %T", i, item)
			}
			rw := Rewrite{}
			if s, ok := m["source"].(string); ok {
				rw.Source = s
			}
			if s, ok := m["destination"].(string); ok {
				rw.Destination = s
			}
			cfg.Rewrites = append(cfg.Rewrites, rw)
		}
	case "content":
		m, ok := val.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected object, got %T", val)
		}
		cfg.Content = m
	case "checks":
		m, ok := val.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected object, got %T", val)
		}
		cfg.Checks = m
	case "i18n":
		m, ok := val.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected object, got %T", val)
		}
		if s, ok := m["defaultLocale"].(string); ok {
			cfg.I18n.DefaultLocale = s
		}
		if arr, ok := m["locales"].([]interface{}); ok {
			cfg.I18n.Locales = interfaceStrings(arr)
		}
		if s, ok := m["routing"].(string); ok {
			cfg.I18n.Routing = s
		}
	case "versions":
		m, ok := val.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected object, got %T", val)
		}
		if s, ok := m["current"].(string); ok {
			cfg.Versions.Current = s
		}
		if arr, ok := m["versions"].([]interface{}); ok {
			cfg.Versions.Versions = interfaceStrings(arr)
		}
		if b, ok := m["banner"].(bool); ok {
			cfg.Versions.Banner = &b
		}
	case "output":
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", val)
		}
		cfg.Output = s
	default:
		// Unknown config keys are silently ignored for forward compatibility
	}
	return nil
}
