package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests run the real `krate` binary against a real project and talk to it
// over the real protocols: JSON-RPC on stdio, and Streamable HTTP.

var (
	krateBinOnce sync.Once
	krateBinPath string
	krateBinErr  error
)

func buildKrateBinary(t *testing.T) string {
	t.Helper()
	krateBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "krate-mcp-e2e-*")
		if err != nil {
			krateBinErr = err
			return
		}
		krateBinPath = filepath.Join(dir, "krate")
		if runtime.GOOS == "windows" {
			krateBinPath += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", krateBinPath, "../../cmd/krate")
		if out, err := cmd.CombinedOutput(); err != nil {
			krateBinErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})
	if krateBinErr != nil {
		t.Fatal(krateBinErr)
	}
	return krateBinPath
}

func TestMain(m *testing.M) {
	code := m.Run()
	if krateBinPath != "" {
		_ = os.RemoveAll(filepath.Dir(krateBinPath))
	}
	os.Exit(code)
}

func e2eProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	port := freeTCPPort(t)
	files := map[string]string{
		"krate.config.ts":       fmt.Sprintf("export default { devServer: { port: %d, open: false } };\n", port),
		"package.json":          `{"name":"e2e-site","version":"0.0.1"}`,
		".env":                  "API_TOKEN=tok-0123456789-SECRET\nKRATE_PUBLIC_SITE=shop\n",
		".krate/mcp.json":       `{"allowTasks":["build","check","dev"]}`,
		"src/pages/_layout.tsx": "export default function Layout({ children }) { return <div>{children}</div>; }\n",
		"src/pages/index.tsx": `import Counter from '../components/Counter';
export default function Home() {
  return <main><Head><title>E2E home</title></Head><h1>Home</h1><Counter /></main>;
}
`,
		"src/pages/about.tsx":        "export default function About() { return <main><Head><title>About</title></Head><h1>About</h1></main>; }\n",
		"src/components/Counter.tsx": counterSrc,
		"src/api/ping.ts":            "export function GET() { return Response.json({ ok: process.env.API_TOKEN ? 1 : 0 }); }\n",
		"src/actions/todo.ts":        "'use server';\nexport async function addTodo(fd) { return 1; }\n",
		"middleware.ts":              "export function middleware() {}\nexport const config = { matcher: ['/admin/:path*'] };\n",
	}
	for rel, c := range files {
		writeFile(t, root, rel, c)
	}
	return root
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// stdioClient drives `krate mcp` over stdin/stdout.
type stdioClient struct {
	t      *testing.T
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	nextID int
}

func startStdio(t *testing.T, root string) *stdioClient {
	t.Helper()
	bin := buildKrateBinary(t)
	cmd := exec.Command(bin, "mcp", root)
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &stdioClient{t: t, cmd: cmd, in: in, out: bufio.NewReaderSize(out, 1<<20)}
	t.Cleanup(func() {
		_ = in.Close()
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
		}
	})
	c.rpc("initialize", map[string]any{"protocolVersion": "2025-06-18"})
	return c
}

func (c *stdioClient) rpc(method string, params any) map[string]any {
	c.t.Helper()
	c.nextID++
	id := c.nextID
	req, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if _, err := c.in.Write(append(req, '\n')); err != nil {
		c.t.Fatal(err)
	}
	for {
		line, err := c.out.ReadBytes('\n')
		if err != nil {
			c.t.Fatalf("reading response to %s: %v", method, err)
		}
		var m map[string]any
		if json.Unmarshal(line, &m) != nil {
			c.t.Fatalf("stdout carried a non-JSON line (protocol stream must stay clean): %q", line)
		}
		if m["id"] == float64(id) {
			return m
		}
	}
}

func (c *stdioClient) tool(name string, args map[string]any) (map[string]any, string, bool) {
	c.t.Helper()
	r := c.rpc("tools/call", map[string]any{"name": name, "arguments": args})
	if r["error"] != nil {
		c.t.Fatalf("%s: rpc error %v", name, r["error"])
	}
	res := asMap(r["result"])
	var text string
	for _, ct := range res["content"].([]any) {
		if cm := asMap(ct); cm["type"] == "text" {
			text += cm["text"].(string)
		}
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(text), &m)
	return m, text, res["isError"] == true
}

func TestE2EStdioAgainstARealProject(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the krate binary")
	}
	root := e2eProject(t)
	c := startStdio(t, root)

	list := c.rpc("tools/list", nil)
	names := map[string]bool{}
	for _, tl := range asMap(list["result"])["tools"].([]any) {
		names[asMap(tl)["name"].(string)] = true
	}
	for _, want := range insightTools {
		if !names[want] {
			t.Errorf("tools/list lacks %s", want)
		}
	}
	if len(names) < 55 {
		t.Errorf("expected at least 55 tools, got %d", len(names))
	}

	meta, _, isErr := c.tool("get_project_metadata", nil)
	if isErr || asMap(meta["package"])["name"] != "e2e-site" || meta["middleware"] != "middleware.ts" {
		t.Fatalf("metadata = %v", meta)
	}
	if routes := asMap(meta["routes"]); routes["count"] != float64(2) {
		t.Errorf("routes = %v", routes)
	}

	if _, text, isErr := c.tool("build", nil); isErr {
		t.Fatalf("build: %s", text)
	}
	ex, _, _ := c.tool("explain_page", map[string]any{"route": "/"})
	if ex["hydratesComponents"] != true || asMap(ex["built"])["shipsJavaScript"] != true {
		t.Errorf("explain_page = %v", ex)
	}
	mw, _, _ := c.tool("list_middleware", map[string]any{"path": "/admin/users"})
	if asMap(mw["pathCheck"])["runs"] != true {
		t.Errorf("middleware = %v", mw)
	}
	api, _, _ := c.tool("list_api_routes", nil)
	if api["total"] != float64(1) {
		t.Errorf("api = %v", api)
	}

	// Secrets: the env file is never readable, and env_check names only.
	_, text, _ := c.tool("env_check", nil)
	if strings.Contains(text, "tok-0123456789-SECRET") {
		t.Fatalf("env_check leaked a secret: %s", text)
	}
	if _, text, isErr := c.tool("read_file", map[string]any{"path": ".env"}); !isErr || strings.Contains(text, "tok-0123456789") {
		t.Errorf("read_file on .env must be refused: %v %s", isErr, text)
	}
	if _, text, _ := c.tool("get_config", map[string]any{"prefix": "devServer"}); strings.Contains(text, root) {
		t.Errorf("get_config leaked the root path: %s", text)
	}

	// Live dev server: start, break a file, read the error, undo, read the log.
	if _, text, isErr := c.tool("dev_server", map[string]any{"action": "start"}); isErr || !strings.Contains(text, "dryRun") {
		t.Fatalf("dev_server dry run: %s", text)
	}
	started, text, isErr := c.tool("dev_server", map[string]any{"action": "start", "apply": true})
	if isErr || started["started"] != true || started["url"] == nil {
		t.Fatalf("dev_server start: %s", text)
	}
	defer c.tool("dev_server", map[string]any{"action": "stop", "apply": true})

	waitForTool(t, 60*time.Second, func() bool {
		st, _, _ := c.tool("dev_server_status", nil)
		return st["running"] == true && st["buildOk"] == true
	})
	errs, _, _ := c.tool("get_errors", nil)
	if errs["source"] != "dev-server" || errs["buildOk"] != true {
		t.Fatalf("clean get_errors = %v", errs)
	}
	if _, text, isErr := c.tool("edit_file", map[string]any{"route": "src/pages/about.tsx", "apply": true,
		"find": "export default function About", "replace": "import Gone from './gone';\nexport default function About"}); isErr {
		t.Fatalf("edit_file: %s", text)
	}
	waitForTool(t, 30*time.Second, func() bool {
		e, _, _ := c.tool("get_errors", map[string]any{"kind": "build"})
		return e["buildOk"] == false
	})
	broken, _, _ := c.tool("get_errors", map[string]any{"kind": "build"})
	first := asMap(broken["errors"].([]any)[0])
	if first["file"] != "src/pages/about.tsx" || first["line"] != float64(1) || !strings.Contains(first["message"].(string), "cannot resolve") {
		t.Errorf("live build error = %v", first)
	}
	if _, text, isErr := c.tool("undo", map[string]any{"apply": true}); isErr {
		t.Fatalf("undo: %s", text)
	}
	waitForTool(t, 30*time.Second, func() bool {
		e, _, _ := c.tool("get_errors", map[string]any{"kind": "build"})
		return e["buildOk"] == true
	})
	logs, _, _ := c.tool("get_logs", map[string]any{"grep": "cannot resolve", "lines": 5})
	if len(logs["lines"].([]any)) == 0 || strings.Contains(fmt.Sprint(logs["lines"]), root) {
		t.Errorf("logs = %v", logs)
	}

	// The audit log recorded the calls, including the destructive ones.
	data, _ := os.ReadFile(filepath.Join(root, ".krate", "mcp-audit.jsonl"))
	for _, tool := range []string{"dev_server", "get_errors", "edit_file", "undo"} {
		if !strings.Contains(string(data), `"tool":"`+tool+`"`) {
			t.Errorf("audit log has no %s record", tool)
		}
	}
	if strings.Contains(string(data), "tok-0123456789") {
		t.Error("audit log leaked a secret")
	}
}

func waitForTool(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the dev server")
}

func TestE2EHTTPAgainstARealProject(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the krate binary")
	}
	root := e2eProject(t)
	bin := buildKrateBinary(t)
	port := freeTCPPort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "mcp", root, "--http", "--port", fmt.Sprint(port), "--token", "e2e-token-123456")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForTool(t, 20*time.Second, func() bool {
		resp, err := http.Get(base + "/healthz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == 200
	})

	post := func(token string, body any) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", base+"/mcp", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		return resp.StatusCode, m
	}
	callTool := func(name string, args map[string]any) (map[string]any, bool) {
		status, m := post("e2e-token-123456", map[string]any{"jsonrpc": "2.0", "id": 7, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
		if status != 200 || m["result"] == nil {
			t.Fatalf("%s: status %d body %v", name, status, m)
		}
		res := asMap(m["result"])
		var out map[string]any
		_ = json.Unmarshal([]byte(asMap(res["content"].([]any)[0])["text"].(string)), &out)
		return out, res["isError"] == true
	}

	if status, _ := post("", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}); status != http.StatusUnauthorized {
		t.Errorf("unauthenticated tools/list = %d, want 401", status)
	}
	status, list := post("e2e-token-123456", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if status != 200 || len(asMap(list["result"])["tools"].([]any)) < 55 {
		t.Fatalf("tools/list = %d", status)
	}

	meta, isErr := callTool("get_project_metadata", nil)
	if isErr || asMap(meta["package"])["name"] != "e2e-site" {
		t.Fatalf("metadata = %v", meta)
	}
	// With no dev server, get_errors falls back to a build and works over HTTP.
	errs, isErr := callTool("get_errors", nil)
	if isErr || errs["source"] != "build" || errs["buildOk"] != true {
		t.Fatalf("get_errors = %v", errs)
	}
	isl, _ := callTool("list_islands", nil)
	if isl["total"].(float64) < 1 {
		t.Errorf("islands = %v", isl)
	}
}
