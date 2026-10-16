package check

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/astjson"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
)

// customRuleGlobal is the esbuild IIFE global that receives a custom rule
// module's namespace.
const customRuleGlobal = "__krateCheckRule"

// customBundleCache memoizes the esbuild-bundled IIFE for each custom rule
// module path (bundling happens once per file; a fresh VM runs per invocation
// so concurrent checks are safe).
var customBundleCache sync.Map // absPath -> string or error

// runCustomRules executes every custom rule module configured in
// `checks.custom` against each page. A custom module exports either:
//
//	export default function check(page, krate) { return [ ...findings ] }
//	export function check(page, krate) { ... }
//
// where page is { route, source, html, jsBytes, ast } and a finding is
// { rule, message, severity?, line?, col?, hint? }. Rules run in the embedded
// QuickJS runtime - no Node, no subprocess.
func runCustomRules(cfg Config, pages []Page) ([]Finding, error) {
	mods := cfg.customModules()
	if len(mods) == 0 {
		return nil, nil
	}

	var out []Finding
	for _, mod := range mods {
		bundle, err := bundleCustomRule(mod)
		if err != nil {
			return out, err
		}
		for i := range pages {
			fs, err := runCustomRule(bundle, mod, cfg, &pages[i])
			if err != nil {
				return out, fmt.Errorf("custom check rule %s: %w", filepath.Base(mod), err)
			}
			out = append(out, fs...)
		}
	}
	return out, nil
}

// runCustomRule evaluates one module's check function against a single page.
func runCustomRule(bundle, mod string, cfg Config, p *Page) ([]Finding, error) {
	rt, err := jsruntime.New()
	if err != nil {
		return nil, err
	}
	defer rt.Close()
	if len(cfg.Env) > 0 {
		_ = rt.SetEnv(cfg.Env)
	}
	rt.SetLogPrefix("[check:" + filepath.Base(mod) + "]")

	if _, err := rt.Execute(bundle); err != nil {
		return nil, fmt.Errorf("loading rule bundle: %w", err)
	}

	pageJSON, err := marshalCheckPage(p)
	if err != nil {
		return nil, err
	}
	metaJSON, err := json.Marshal(map[string]any{
		"root":    cfg.Root,
		"version": "",
	})
	if err != nil {
		return nil, err
	}

	call := fmt.Sprintf(`
(function() {
  try {
    var mod = %[1]s || {};
    var fn = (mod.default && (typeof mod.default === 'function' ? mod.default : mod.default.check)) || mod.check;
    if (typeof fn !== 'function') return JSON.stringify({ skip: true });
    var out = fn(%[2]s, { root: %[3]s.root });
    if (out && typeof out.then === 'function') return JSON.stringify({ error: 'async check rules are not supported' });
    return JSON.stringify({ findings: out || [] });
  } catch (e) {
    return JSON.stringify({ error: (e && e.message) || String(e) });
  }
})()
`, customRuleGlobal, string(pageJSON), string(metaJSON))

	res, err := rt.Execute(call)
	if err != nil {
		return nil, err
	}
	s, ok := res.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected rule result type %T", res)
	}
	var msg struct {
		Skip     bool              `json:"skip"`
		Error    string            `json:"error"`
		Findings []json.RawMessage `json:"findings"`
	}
	if err := json.Unmarshal([]byte(s), &msg); err != nil {
		return nil, fmt.Errorf("decoding rule result: %w", err)
	}
	if msg.Skip {
		return nil, nil
	}
	if msg.Error != "" {
		return nil, fmt.Errorf("%s", msg.Error)
	}

	var out []Finding
	for _, raw := range msg.Findings {
		var f struct {
			Rule     string `json:"rule"`
			Message  string `json:"message"`
			Severity string `json:"severity"`
			Line     int    `json:"line"`
			Col      int    `json:"col"`
			Hint     string `json:"hint"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("decoding finding: %w", err)
		}
		if f.Message == "" {
			continue
		}
		sev := ParseSeverity(f.Severity)
		if sev == Off {
			sev = Error
		}
		rule := f.Rule
		if rule == "" {
			rule = "custom/" + filepath.Base(mod)
		}
		out = append(out, Finding{
			Rule:     rule,
			Category: "custom",
			Severity: sev,
			Route:    p.Route,
			File:     p.RelSource,
			Line:     f.Line,
			Col:      f.Col,
			Message:  f.Message,
			Hint:     f.Hint,
		})
	}
	return out, nil
}

// marshalCheckPage serializes the page surface a custom rule sees. The AST is
// the kind-tagged astjson document so rule authors get a stable shape.
func marshalCheckPage(p *Page) ([]byte, error) {
	doc := map[string]any{
		"route":   p.Route,
		"source":  p.RelSource,
		"html":    p.HTML,
		"jsBytes": p.JSBytes,
	}
	if p.Program != nil {
		enc, err := astjson.EncodeProgram(p.Program)
		if err != nil {
			return nil, err
		}
		doc["ast"] = json.RawMessage(enc)
	}
	return json.Marshal(doc)
}

// bundleCustomRule bundles a custom rule module into a self-contained IIFE that
// assigns its namespace to customRuleGlobal. Results are cached per path.
func bundleCustomRule(module string) (string, error) {
	absPath := module
	if !filepath.IsAbs(absPath) {
		abs, err := filepath.Abs(absPath)
		if err != nil {
			return "", err
		}
		absPath = abs
	}
	if v, ok := customBundleCache.Load(absPath); ok {
		if err, isErr := v.(error); isErr {
			return "", err
		}
		return v.(string), nil
	}

	if _, err := os.Stat(absPath); err != nil {
		err = fmt.Errorf("custom check rule not found at %s", absPath)
		customBundleCache.Store(absPath, err)
		return "", err
	}

	result := api.Build(api.BuildOptions{
		EntryPoints: []string{absPath},
		Bundle:      true,
		Platform:    api.PlatformNode,
		Format:      api.FormatIIFE,
		GlobalName:  customRuleGlobal,
		LogLevel:    api.LogLevelError,
		Write:       false,
	})
	if len(result.Errors) > 0 {
		var sb strings.Builder
		for _, e := range result.Errors {
			sb.WriteString(e.Text)
			sb.WriteString("\n")
		}
		err := fmt.Errorf("bundling custom check rule:\n%s", sb.String())
		customBundleCache.Store(absPath, err)
		return "", err
	}
	if len(result.OutputFiles) == 0 {
		err := fmt.Errorf("custom check rule bundle produced no output")
		customBundleCache.Store(absPath, err)
		return "", err
	}
	bundle := string(result.OutputFiles[0].Contents)
	customBundleCache.Store(absPath, bundle)
	return bundle, nil
}
