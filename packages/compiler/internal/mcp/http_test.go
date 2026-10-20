package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPTransport(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }\n",
	})
	ts := httptest.NewServer(svc.HTTPHandler("secret"))
	defer ts.Close()

	post := func(body, token string) (*http.Response, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		var decoded map[string]any
		if len(bytes.TrimSpace(data)) > 0 {
			_ = json.Unmarshal(data, &decoded)
		}
		return resp, decoded
	}

	if resp, _ := post(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", resp.StatusCode)
	}

	resp, decoded := post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`, "secret")
	if resp.StatusCode != http.StatusOK || decoded == nil || decoded["result"] == nil {
		t.Fatalf("initialize = status %d body %v", resp.StatusCode, decoded)
	}
	if resp.Header.Get("Mcp-Session-Id") == "" {
		t.Error("missing Mcp-Session-Id")
	}

	resp, _ = post(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, "secret")
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("notification status = %d, want 202", resp.StatusCode)
	}

	_, decoded = post(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, "secret")
	result, _ := decoded["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if len(tools) == 0 {
		t.Fatalf("tools/list empty: %v", decoded)
	}
}

func TestHTTPStructuredResult(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }\n",
	})
	ts := httptest.NewServer(svc.HTTPHandler(""))
	defer ts.Close()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_files","arguments":{"path":"src/pages/index.tsx"}}}`
	resp, err := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	result, _ := decoded["result"].(map[string]any)
	if result["structuredContent"] == nil {
		t.Fatalf("missing structuredContent: %v", decoded)
	}
}

func TestHTTPSSEHeaders(t *testing.T) {
	svc := newTestService(t, map[string]string{})
	ts := httptest.NewServer(svc.HTTPHandler(""))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/mcp", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("SSE status/type = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}

func TestHTTPPersistentSessionProgressAndReconnect(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }\n",
	})
	ts := httptest.NewServer(svc.HTTPHandler(""))
	defer ts.Close()

	post := func(body, sid string) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(body))
		if sid != "" {
			req.Header.Set("Mcp-Session-Id", sid)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, data
	}
	resp, _ := post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`, "")
	sid := resp.Header.Get("Mcp-Session-Id")
	if sid == "" {
		t.Fatal("missing session id")
	}
	post(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, sid)
	post(`{"jsonrpc":"2.0","id":2,"method":"resources/subscribe","params":{"uri":"krate://manifest"}}`, sid)
	post(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search_project","arguments":{"query":"Page"},"_meta":{"progressToken":"p-http"}}}`, sid)
	post(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"edit_file","arguments":{"route":"src/pages/index.tsx","find":"Hi","replace":"Hello","apply":true}}}`, sid)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/mcp", nil)
	req.Header.Set("Mcp-Session-Id", sid)
	req.Header.Set("Last-Event-ID", "0")
	sresp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer sresp.Body.Close()
	scanner := bufio.NewScanner(sresp.Body)
	var events []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			events = append(events, strings.TrimPrefix(line, "data: "))
			if len(events) >= 6 {
				break
			}
		}
	}
	joined := strings.Join(events, "\n")
	if !strings.Contains(joined, "notifications/progress") {
		t.Fatalf("reconnected SSE did not replay progress:\n%s", joined)
	}
	if !strings.Contains(joined, "notifications/resources/updated") {
		t.Fatalf("reconnected SSE did not replay resource update:\n%s", joined)
	}
}
