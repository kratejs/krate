package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"io"
	"slices"
	"sort"
)

func TestDevHubSnapshotReportsBuildAndBrowserState(t *testing.T) {
	hub := NewDevHub()
	snap := hub.Snapshot()
	if !snap.BuildOK || !snap.Built || len(snap.ClientErrors) != 0 || len(snap.Diagnostics) != 0 {
		t.Fatalf("fresh hub = %+v", snap)
	}
	hub.Publish(DevEvent{Type: "reload", Diagnostics: []diag.Diagnostic{{File: "a.tsx", Line: 3, Message: "boom", Code: "KR7115"}}})
	hub.Publish(DevEvent{Type: "client-error", ClientError: &ClientError{Message: "TypeError: x", URL: "/a"}})
	snap = hub.Snapshot()
	if snap.BuildOK || len(snap.Diagnostics) != 1 || snap.Diagnostics[0].Code != "KR7115" {
		t.Fatalf("a failed build must stay failed after a browser error: %+v", snap)
	}
	if len(snap.ClientErrors) != 1 || snap.ClientErrors[0].Message != "TypeError: x" || snap.ClientErrors[0].Time.IsZero() {
		t.Fatalf("client errors = %+v", snap.ClientErrors)
	}
	hub.Publish(DevEvent{Type: "reload", Routes: []string{"/a"}})
	snap = hub.Snapshot()
	if !snap.BuildOK || len(snap.Diagnostics) != 0 || snap.Rebuilds != 2 || snap.LastEventAt == nil {
		t.Fatalf("after the fix = %+v", snap)
	}
	if len(snap.ClientErrors) != 1 {
		t.Error("browser errors are kept until the server restarts")
	}
}

// Regression: a browser error used to replace the hub's current event, so
// /__krate/status and late-connecting tabs reported a failing build as OK.
func TestClientErrorDoesNotReplaceTheCurrentBuildState(t *testing.T) {
	hub := NewDevHub()
	hub.Publish(DevEvent{Type: "reload", Diagnostics: []diag.Diagnostic{{Message: "build failed"}}})
	hub.Publish(DevEvent{Type: "client-error", ClientError: &ClientError{Message: "runtime"}})
	cur, ok := hub.Current()
	if !ok || cur.BuildOK || len(cur.Diagnostics) != 1 {
		t.Fatalf("Current() after a client error = %+v, want the failed build", cur)
	}
	ch, cancel := hub.Subscribe()
	defer cancel()
	hub.Publish(DevEvent{Type: "client-error", ClientError: &ClientError{Message: "again"}})
	select {
	case ev := <-ch:
		if ev.Type != "client-error" || ev.ClientError.Message != "again" {
			t.Errorf("subscribers still receive browser errors: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive the client error")
	}
}

func TestDevHubKeepsOnlyTheLatestClientErrors(t *testing.T) {
	hub := NewDevHub()
	for i := 0; i < maxClientErrors+25; i++ {
		hub.Publish(DevEvent{Type: "client-error", ClientError: &ClientError{Message: fmt.Sprint("e", i)}})
	}
	errs := hub.Snapshot().ClientErrors
	if len(errs) != maxClientErrors || errs[0].Message != "e25" || errs[len(errs)-1].Message != fmt.Sprint("e", maxClientErrors+24) {
		t.Fatalf("kept %d errors, first %q last %q", len(errs), errs[0].Message, errs[len(errs)-1].Message)
	}
}

func TestDevServerRecordRoundTripAndOwnership(t *testing.T) {
	root := t.TempDir()
	if _, ok := ReadDevServerRecord(root); ok {
		t.Fatal("no record yet")
	}
	remove := writeDevServerRecord(root, DevServerRecord{Host: "localhost", Port: 3001, URL: "http://localhost:3001", StartedAt: time.Now()})
	rec, ok := ReadDevServerRecord(root)
	if !ok || rec.PID != os.Getpid() || rec.Port != 3001 {
		t.Fatalf("record = %+v ok=%v", rec, ok)
	}
	// A record that names another process is left alone on shutdown.
	other, _ := json.Marshal(DevServerRecord{PID: os.Getpid() + 1, Port: 4000})
	if err := os.WriteFile(DevServerRecordPath(root), other, 0o644); err != nil {
		t.Fatal(err)
	}
	remove()
	if _, ok := ReadDevServerRecord(root); !ok {
		t.Error("removed a record that belongs to another process")
	}
	remove2 := writeDevServerRecord(root, DevServerRecord{Port: 3002})
	remove2()
	if _, ok := ReadDevServerRecord(root); ok {
		t.Error("record should be removed by its owner")
	}
	if err := os.WriteFile(DevServerRecordPath(root), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadDevServerRecord(root); ok {
		t.Error("a corrupt record must read as absent")
	}
}

func TestTeeDevLogStampsLinesStripsANSIAndRestoresStreams(t *testing.T) {
	root := t.TempDir()
	origOut, origErr := os.Stdout, os.Stderr
	// Capture what still reaches the original streams.
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr

	stop := TeeDevLog(root)
	fmt.Fprint(os.Stdout, "\x1b[32mgreen line\x1b[0m\npartial ")
	fmt.Fprint(os.Stderr, "err line\r\n")
	fmt.Fprint(os.Stdout, "rest\n")
	printf("%sprogress%s\n", cGreen, cReset)
	stop()

	if os.Stdout != wOut || os.Stderr != wErr {
		t.Error("streams were not restored")
	}
	os.Stdout, os.Stderr = origOut, origErr
	_ = wOut.Close()
	_ = wErr.Close()
	var outBuf, errBuf bytes.Buffer
	_, _ = outBuf.ReadFrom(rOut)
	_, _ = errBuf.ReadFrom(rErr)
	if !strings.Contains(outBuf.String(), "\x1b[32mgreen line") || !strings.Contains(errBuf.String(), "err line") {
		t.Errorf("console output was swallowed: out=%q err=%q", outBuf.String(), errBuf.String())
	}

	data, err := os.ReadFile(DevLogPath(root))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{"green line", "err line", "partial rest", "progress"}
	if len(lines) != len(want) {
		t.Fatalf("log = %q", lines)
	}
	got := map[string]bool{}
	for _, l := range lines {
		if strings.Contains(l, "\x1b") || strings.Contains(l, "\r") {
			t.Errorf("control characters in %q", l)
		}
		ts, text, ok := strings.Cut(l, " ")
		if !ok {
			t.Fatalf("line without a timestamp: %q", l)
		}
		if _, err := time.Parse(DevLogTimeFormat, ts); err != nil {
			t.Errorf("bad timestamp %q: %v", ts, err)
		}
		got[text] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("log lacks %q: %q", w, lines)
		}
	}
}

func TestListActionsScansUseServerModules(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"src/pages/index.tsx":  "export default function P() { return <h1>x</h1>; }",
		"src/actions/a.ts":     "'use server';\nexport async function save(fd) { return 1; }\nexport const del = async () => 2;\n",
		"src/actions/plain.ts": "export function nope() {}\n",
		"node_modules/x/a.ts":  "'use server';\nexport function skipped() {}\n",
	})
	cfg := config.Default()
	cfg.Resolve(root)
	b := New(root, cfg)
	acts, err := b.ListActions()
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 2 || acts[0].Export != "del" || acts[1].Export != "save" || acts[0].Source != "src/actions/a.ts" {
		t.Fatalf("actions = %+v", acts)
	}
	if acts[0].ID != actionID("src/actions/a.ts", "del") {
		t.Errorf("ids must match the ones the server serves: %+v", acts[0])
	}
}

func TestDevServerPublishesStateAndDiscoveryRecord(t *testing.T) {
	root, cfg := secProject(t)
	cfg.Compress.Gzip = false
	cfg.Redirects = nil
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	provideRuntime(t, root)
	port := freePort(t)
	cfg.Server.Port = port
	hub := NewDevHub()
	hub.Publish(DevEvent{Type: "reload", Diagnostics: []diag.Diagnostic{{File: "src/pages/x.tsx", Line: 2, Message: "broken", Code: "KR7115"}}})
	errCh := make(chan error, 1)
	go func() { errCh <- ServeDev(root, cfg, hub, time.Now()) }()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Registered first so it runs after the server has been stopped.
	t.Cleanup(func() {
		if _, ok := ReadDevServerRecord(root); ok {
			t.Error("the discovery record must be removed when the dev server stops")
		}
	})
	deadline := time.Now().Add(30 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("dev server did not start")
		}
		select {
		case err := <-errCh:
			t.Fatalf("dev server exited: %v", err)
		default:
		}
		if resp, err := http.Get(base + "/"); err == nil {
			_ = resp.Body.Close()
			registerServerStop(t, errCh)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	rec, ok := ReadDevServerRecord(root)
	if !ok || rec.Port != port || rec.PID != os.Getpid() || !strings.HasPrefix(rec.URL, "http://") {
		t.Fatalf("discovery record = %+v ok=%v (port %d)", rec, ok, port)
	}

	post, _ := http.NewRequest("POST", base+"/__krate/client-error", strings.NewReader(`{"message":"TypeError: boom","url":"/p","line":4,"col":2}`))
	if resp, err := http.DefaultClient.Do(post); err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("client-error: %v %v", resp, err)
	}
	resp, err := http.Get(base + "/__krate/state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var snap DevSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if snap.BuildOK || len(snap.Diagnostics) != 1 || snap.Diagnostics[0].Code != "KR7115" || len(snap.ClientErrors) != 1 || snap.ClientErrors[0].Line != 4 {
		t.Fatalf("state = %+v", snap)
	}

	// The state endpoint is a dev endpoint: same guard as /__krate/status.
	req, _ := http.NewRequest("GET", base+"/__krate/state", nil)
	req.Host = "evil.example:80"
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != http.StatusForbidden {
		t.Errorf("DNS rebinding to /__krate/state = %v %v, want 403", r, err)
	}
	req, _ = http.NewRequest("GET", base+"/__krate/state", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site /__krate/state = %v %v, want 403", r, err)
	}
}

// A line left open on one stream must not swallow a line from the other.
func TestTeeDevLogKeepsStreamLinesApart(t *testing.T) {
	root := t.TempDir()
	origOut, origErr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	_, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr
	defer func() { os.Stdout, os.Stderr = origOut, origErr }()

	stop := TeeDevLog(root)
	fmt.Fprint(os.Stdout, "partial ")
	// The tee has read it once it reaches the original stream. The pause only
	// lets the tee finish recording it, so a shared buffer shows up every time.
	if _, err := io.ReadFull(rOut, make([]byte, len("partial "))); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	fmt.Fprint(os.Stderr, "err line\n")
	time.Sleep(50 * time.Millisecond)
	fmt.Fprint(os.Stdout, "rest\nunfinished")
	stop()

	data, err := os.ReadFile(DevLogPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var text []string
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		_, line, _ := strings.Cut(l, " ")
		text = append(text, line)
	}
	sort.Strings(text)
	if want := []string{"err line", "partial rest", "unfinished"}; !slices.Equal(text, want) {
		t.Errorf("log lines = %q, want %q", text, want)
	}
}
