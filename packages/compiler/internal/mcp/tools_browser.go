package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/execx"
)

// The browser tools drive a Playwright install that the project already has.
// Krate adds no browser dependency: if node or playwright is missing the tools
// say exactly what to install. All traffic is confined to the loopback origin
// of the app under test, so an expression cannot send data elsewhere.

const browserScript = `
const opts = JSON.parse(process.env.KRATE_BROWSER_OPTS);
let pw;
for (const name of ['playwright', 'playwright-core', '@playwright/test']) {
  try { pw = require(require.resolve(name, { paths: [process.cwd()] })); break; } catch (e) {}
}
if (!pw || !pw.chromium) { console.log('KRATE_RESULT ' + JSON.stringify({ missing: 'playwright' })); process.exit(0); }
(async () => {
  const out = { console: [], pageErrors: [], failedRequests: [], badResponses: [], blocked: 0 };
  let browser;
  try {
    browser = await pw.chromium.launch({ headless: true });
  } catch (e) {
    console.log('KRATE_RESULT ' + JSON.stringify({ launchError: String(e && e.message || e).split('\n').slice(0, 6).join('\n') }));
    process.exit(0);
  }
  try {
    const ctx = await browser.newContext({ viewport: { width: opts.width, height: opts.height } });
    const page = await ctx.newPage();
    const origin = new URL(opts.url).origin;
    await page.route('**/*', (route) => {
      let u; try { u = new URL(route.request().url()); } catch (e) { return route.abort(); }
      if (u.protocol === 'data:' || u.protocol === 'blob:' || u.protocol === 'about:' || u.origin === origin) return route.continue();
      out.blocked++; return route.abort();
    });
    page.on('console', (m) => { if (out.console.length < 100) out.console.push({ type: m.type(), text: m.text().slice(0, 500), url: m.location().url, line: m.location().lineNumber }); });
    page.on('pageerror', (e) => { if (out.pageErrors.length < 50) out.pageErrors.push({ message: String(e.message || e).slice(0, 1000), stack: String(e.stack || '').split('\n').slice(0, 8).join('\n') }); });
    page.on('requestfailed', (r) => { if (out.failedRequests.length < 50 && !/ERR_FAILED/.test((r.failure() || {}).errorText || '') ) out.failedRequests.push({ url: r.url(), error: (r.failure() || {}).errorText }); });
    page.on('response', (r) => { if (r.status() >= 400 && out.badResponses.length < 50) out.badResponses.push({ url: r.url(), status: r.status() }); });
    const resp = await page.goto(opts.url, { waitUntil: 'load', timeout: opts.timeout });
    out.status = resp ? resp.status() : null;
    try { await page.waitForLoadState('networkidle', { timeout: Math.min(opts.timeout, 5000) }); } catch (e) {}
    if (opts.waitFor) await page.waitForSelector(opts.waitFor, { timeout: opts.timeout });
    out.title = await page.title();
    out.finalUrl = page.url();
    out.timing = await page.evaluate(() => {
      const n = performance.getEntriesByType('navigation')[0];
      const res = performance.getEntriesByType('resource');
      return {
        domContentLoadedMs: n ? Math.round(n.domContentLoadedEventEnd) : null,
        loadMs: n ? Math.round(n.loadEventEnd) : null,
        resources: res.length,
        jsBytes: res.filter((r) => r.initiatorType === 'script' || /\.m?js(\?|$)/.test(r.name)).reduce((a, r) => a + (r.encodedBodySize || 0), 0),
        scripts: document.scripts.length,
      };
    });
    if (opts.expression) {
      try {
        // A promise that never settles must not outlive timeoutMs.
        let timer;
        const expired = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('the expression did not finish within ' + opts.timeout + ' ms')), opts.timeout); });
        const v = await Promise.race([page.evaluate(opts.expression), expired]).finally(() => clearTimeout(timer));
        let s = JSON.stringify(v === undefined ? null : v);
        if (s && s.length > 32768) { out.evalTruncated = true; s = s.slice(0, 32768); }
        out.evalResult = s;
      } catch (e) { out.evalError = String(e && e.message || e).slice(0, 2000); }
    }
    if (opts.aria) {
      try { out.ariaSnapshot = (await page.locator('body').ariaSnapshot()).slice(0, 12000); } catch (e) { out.ariaError = String(e.message || e).slice(0, 300); }
    }
    if (opts.screenshot) {
      const buf = await page.screenshot({ fullPage: !!opts.fullPage, type: 'png' });
      out.screenshot = buf.length <= 1500000 ? buf.toString('base64') : null;
      out.screenshotBytes = buf.length;
    }
  } catch (e) {
    out.error = String(e && e.message || e).split('\n').slice(0, 6).join('\n');
  } finally {
    await browser.close();
  }
  console.log('KRATE_RESULT ' + JSON.stringify(out));
})();
`

// staticServer serves the build output on a loopback port for a browser check
// when no dev server is running. It handles clean URLs through the directory
// index; request-time (SSR) pages need a running server.
func (s *Service) startStaticServer() (base string, stop func(), err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	srv := &http.Server{Handler: http.FileServer(http.Dir(s.outDirAbs())), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return "http://" + ln.Addr().String(), func() { _ = srv.Close() }, nil
}

type browserRun struct {
	Route      string
	Expression string
	WaitFor    string
	Screenshot bool
	FullPage   bool
	Aria       bool
	Width      int
	Height     int
	TimeoutMS  int
}

func (s *Service) runBrowser(ctx context.Context, r browserRun) (map[string]any, string, error) {
	node, err := exec.LookPath("node")
	if err != nil {
		return nil, "", fmt.Errorf("node is not installed; the browser tools need Node.js and a Playwright install in the project (npm i -D playwright && npx playwright install chromium)")
	}
	base := ""
	source := ""
	if t, ok := s.findDevServer(ctx); ok {
		base, source = t.BaseURL, "dev server "+t.Rec.URL
	} else if _, err := os.Stat(s.outDirAbs()); err == nil {
		b, stop, serr := s.startStaticServer()
		if serr != nil {
			return nil, "", serr
		}
		defer stop()
		base, source = b, "static server over the build output (no dev server running; request-time pages are not served)"
	} else {
		return nil, "", fmt.Errorf("nothing to open: no dev server is running and there is no build output; use dev_server (start) or build")
	}
	route := r.Route
	if route == "" {
		route = "/"
	}
	if !strings.HasPrefix(route, "/") {
		return nil, "", fmt.Errorf("route must be a path starting with /, got %q", route)
	}
	u, err := url.Parse(base + route)
	if err != nil {
		return nil, "", err
	}
	opts := map[string]any{
		"url": u.String(), "expression": r.Expression, "waitFor": r.WaitFor, "screenshot": r.Screenshot,
		"fullPage": r.FullPage, "aria": r.Aria, "width": r.Width, "height": r.Height, "timeout": r.TimeoutMS,
	}
	raw, _ := json.Marshal(opts)
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(r.TimeoutMS+30000)*time.Millisecond)
	defer cancel()
	cmd := execx.Command(runCtx, node, "-e", browserScript)
	cmd.Dir = s.root
	cmd.Env = append(os.Environ(), "KRATE_BROWSER_OPTS="+string(raw))
	outBytes, runErr := cmd.CombinedOutput()
	text := string(outBytes)
	idx := strings.LastIndex(text, "KRATE_RESULT ")
	if idx < 0 {
		msg, _ := truncateText(strings.TrimSpace(text), 2000)
		if runErr != nil {
			return nil, "", fmt.Errorf("browser script failed: %v\n%s", runErr, msg)
		}
		return nil, "", fmt.Errorf("browser script printed no result:\n%s", msg)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(text[idx+len("KRATE_RESULT "):])), &res); err != nil {
		return nil, "", fmt.Errorf("reading browser result: %v", err)
	}
	return res, source, nil
}

func (s *Service) browserArgs(args map[string]any) browserRun {
	return browserRun{
		Route:      argString(args, "route"),
		Expression: argString(args, "expression"),
		WaitFor:    argString(args, "waitFor"),
		Screenshot: argBool(args, "screenshot", false),
		FullPage:   argBool(args, "fullPage", false),
		Aria:       argBool(args, "aria", false),
		Width:      clampInt(args, "width", 1280, 3840),
		Height:     clampInt(args, "height", 800, 3840),
		TimeoutMS:  clampInt(args, "timeoutMs", 20000, 90000),
	}
}

func (s *Service) browserTool(ctx context.Context, args map[string]any, needExpression bool) (ToolResult, *rpcError) {
	if r := s.denyIfReadOnly("browser tools"); r != nil {
		return *r, nil
	}
	if !s.taskAllowed("browser") {
		return ErrorResult(`the browser tools load your app in a real browser and run page scripts; add "browser" to allowTasks in .krate/mcp.json to enable them`), nil
	}
	run := s.browserArgs(args)
	if needExpression && run.Expression == "" {
		return ToolResult{}, &rpcError{Code: codeInvalidParams, Message: `missing required argument "expression"`}
	}
	reportProgress(ctx, 0, 1, "driving the browser")
	res, source, err := s.runBrowser(ctx, run)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	reportProgress(ctx, 1, 1, "browser done")
	if res["missing"] == "playwright" {
		return ErrorResult("Playwright is not installed in this project. Install it once with `npm i -D playwright && npx playwright install chromium`, then retry. The browser tools use the project's own install; Krate bundles no browser."), nil
	}
	if le, ok := res["launchError"].(string); ok {
		return ErrorResult("Playwright is installed but Chromium could not start: " + le + "\nRun `npx playwright install chromium` (and `npx playwright install-deps` on Linux)."), nil
	}
	var image *Content
	if b64, ok := res["screenshot"].(string); ok && b64 != "" {
		image = &Content{Type: "image", Data: b64, MIMEType: "image/png"}
	}
	delete(res, "screenshot")
	res["source"] = source
	res["blockedExternalRequests"] = res["blocked"]
	delete(res, "blocked")
	r, rerr := JSONResult(res)
	if rerr != nil {
		return r, rerr
	}
	if image != nil {
		r.Content = append(r.Content, *image)
	}
	if e, ok := res["error"].(string); ok && e != "" {
		r.IsError = true
	}
	return r, nil
}

func (s *Service) registerBrowserTools(srv *Server) {
	props := func(extra map[string]any) map[string]any {
		m := map[string]any{
			"route":      strSchema("Path to open, e.g. /about (default /)"),
			"waitFor":    strSchema("CSS selector to wait for before inspecting"),
			"screenshot": boolSchema("Attach a PNG screenshot (default false)"),
			"fullPage":   boolSchema("Full-page screenshot (default false)"),
			"aria":       boolSchema("Include the accessibility tree snapshot (default false)"),
			"width":      numSchema("Viewport width (default 1280)"),
			"height":     numSchema("Viewport height (default 800)"),
			"timeoutMs":  numSchema("Navigation timeout (default 20000, max 90000)"),
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	s.reg(srv, Tool{
		Name: "page_check",
		Description: "Open a route in a real headless Chromium (via the project's own Playwright install) and report what only a browser can: console messages, uncaught page errors, failed requests and 4xx/5xx responses, load timing and JS bytes, the accessibility tree, and an optional screenshot. " +
			"Uses the running dev server, else serves the build output. Traffic is limited to the app's own origin. Needs \"browser\" in allowTasks and Playwright installed; fails with install instructions otherwise.",
		InputSchema: objSchema(props(nil)),
		Annotations: map[string]any{"title": "Page Check", "readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
		Handler: func(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
			return s.browserTool(ctx, args, false)
		},
	})
	s.reg(srv, Tool{
		Name:        "browser_eval",
		Description: "Open a route in headless Chromium and evaluate a JavaScript expression in the page, returning its JSON result alongside the console and error report of page_check. Use it to verify hydration, read the DOM state or click through behaviour (e.g. document.querySelector('button').click() then read text). The page can only reach its own origin. Needs \"browser\" in allowTasks and Playwright installed.",
		InputSchema: objSchema(props(map[string]any{
			"expression": strSchema("JavaScript expression or arrow function source evaluated in the page, e.g. document.title or () => document.querySelectorAll('button').length"),
		}), "expression"),
		Annotations: map[string]any{"title": "Browser Eval", "readOnlyHint": false, "destructiveHint": false, "idempotentHint": false, "openWorldHint": false},
		Handler: func(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
			return s.browserTool(ctx, args, true)
		},
	})
}
