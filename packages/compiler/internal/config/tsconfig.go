package config

import (
	"fmt"
	"strconv"

	"krate-compiler/internal/lexer"
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
	for _, prop := range obj {
		if err := applyConfigProp(cfg, prop.key, prop.val); err != nil {
			return fmt.Errorf("property %q: %w", prop.key, err)
		}
	}
	return nil
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
	if t.Kind == lexer.Identifier {
		return t.Value, nil
	}
	return "", fmt.Errorf("expected property key (identifier or string), got %q at line %d", t.Value, t.Line)
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
			}
		}
	case "emitReact":
		b, ok := val.(bool)
		if !ok {
			return fmt.Errorf("expected boolean, got %T", val)
		}
		cfg.EmitReact = b
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
			case "math":
				if b, ok := v.(bool); ok {
					cfg.Markdown.Math = b
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
			case "serverComponentRuntime":
				if s, ok := v.(string); ok {
					cfg.SSR.ServerComponentRuntime = s
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
	default:
		// Unknown config keys are silently ignored for forward compatibility
	}
	return nil
}
