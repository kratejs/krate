//go:build linux

package guitest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/sdk/internal/sessionbus"
	"github.com/kratejs/krate/packages/native/sdk/notify"
)

// Tests in this file talk to a real notification daemon (dunst) on a private
// session bus and display. The fake-service tests in the sdk package cover
// protocol edge cases; these check what a real server does with our calls.

// dunstEnv is a running dunst plus helpers to drive it with dunstctl.
type dunstEnv struct {
	*daemon
	dir    string
	closed chan [2]uint32 // server id, reason
}

func startDunst(t *testing.T) *dunstEnv {
	t.Helper()
	needServices(t, "dunst", "dunstctl", "dbus-monitor")
	privateBus(t)
	dir := scratchDir(t)
	// The "dmenu" is a script so tests can choose a context menu entry.
	writeFile(t, filepath.Join(dir, "pick.sh"), "#!/bin/sh\ncat > \""+dir+"/menu.txt\"\ngrep -m1 -F \"$(cat \""+dir+"/pick\")\" \""+dir+"/menu.txt\"\n", 0o755)
	writeFile(t, filepath.Join(dir, "pick"), "none", 0o644)
	writeFile(t, filepath.Join(dir, "dunstrc"), "[global]\n    dmenu = "+filepath.Join(dir, "pick.sh")+"\n    follow = none\n    sticky_history = yes\n", 0o644)
	e := &dunstEnv{dir: dir, closed: make(chan [2]uint32, 16)}
	e.daemon = startDaemon(t, "dunst", "-config", filepath.Join(dir, "dunstrc"))
	waitFor(t, "dunst to own org.freedesktop.Notifications", 8*time.Second, func() bool {
		if e.exited() {
			t.Fatalf("dunst exited early: %s", e.out.String())
		}
		return sessionbus.NameHasOwner("org.freedesktop.Notifications")
	})
	e.watchClosed(t)
	return e
}

// watchClosed runs dbus-monitor as an independent observer of the
// NotificationClosed reason codes. dunst addresses its signals to the sender
// of Notify, so an ordinary match rule on another connection would not see
// them; a monitor does.
func (e *dunstEnv) watchClosed(t *testing.T) {
	t.Helper()
	cmd := exec.Command("dbus-monitor", "--session", "type='signal',interface='org.freedesktop.Notifications',member='NotificationClosed'")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill() // it may have exited already
		_ = cmd.Wait()
	})
	ready := make(chan struct{}, 1)
	go func() {
		sc := bufio.NewScanner(out)
		var nums []uint32
		inSig := false
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "signal "):
				inSig = strings.Contains(line, "member=NotificationClosed")
				nums = nums[:0]
				select {
				case ready <- struct{}{}:
				default:
				}
			case inSig && strings.HasPrefix(strings.TrimSpace(line), "uint32 "):
				var n uint32
				if _, err := fmt.Sscan(strings.TrimPrefix(strings.TrimSpace(line), "uint32 "), &n); err != nil {
					t.Error(err)
				}
				nums = append(nums, n)
				if len(nums) == 2 {
					e.closed <- [2]uint32{nums[0], nums[1]}
					inSig = false
				}
			}
		}
	}()
	// dbus-monitor prints its own NameAcquired signal once it is attached.
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("dbus-monitor did not attach")
	}
}

func (e *dunstEnv) ctl(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("dunstctl", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("dunstctl %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func (e *dunstEnv) displayed(t *testing.T) int {
	t.Helper()
	for _, l := range strings.Split(e.ctl(t, "count"), "\n") {
		if strings.Contains(l, "Currently displayed:") {
			var n int
			fields := strings.Fields(l)
			if _, err := fmt.Sscan(fields[len(fields)-1], &n); err == nil {
				return n
			}
		}
	}
	t.Fatal("cannot read the displayed count")
	return 0
}

// historyEntry is one dunst history record, flattened.
type historyEntry map[string]any

func (e *dunstEnv) history(t *testing.T) []historyEntry {
	t.Helper()
	var doc struct {
		Data [][]map[string]struct {
			Data any `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(e.ctl(t, "history")), &doc); err != nil {
		t.Fatal(err)
	}
	var out []historyEntry
	if len(doc.Data) == 0 {
		return out
	}
	for _, rec := range doc.Data[0] {
		h := historyEntry{}
		for k, v := range rec {
			h[k] = v.Data
		}
		out = append(out, h)
	}
	return out
}

func (e *dunstEnv) setPick(t *testing.T, label string) {
	t.Helper()
	writeFile(t, filepath.Join(e.dir, "pick"), label, 0o644)
}

func waitEvent(t *testing.T, ch chan notify.Event, what string) notify.Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(5 * time.Second):
		t.Fatalf("no notification event: %s", what)
		return notify.Event{}
	}
}

func waitClosed(t *testing.T, e *dunstEnv, what string) [2]uint32 {
	t.Helper()
	select {
	case c := <-e.closed:
		return c
	case <-time.After(5 * time.Second):
		t.Fatalf("no NotificationClosed signal: %s", what)
		return [2]uint32{}
	}
}

func subscribeNotify(t *testing.T) chan notify.Event {
	t.Helper()
	ch := make(chan notify.Event, 16)
	cancel, err := notify.OnEvent(func(e notify.Event) { ch <- e })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)
	eventually(t, "notify subscriber", func() bool { return host.Subscribers("notify") > 0 })
	return ch
}

func TestRealDunstShowsNotification(t *testing.T) {
	e := startDunst(t)
	if !host.SupportsForTest("notify") {
		t.Fatal("notify is not reported as supported while dunst runs")
	}
	icon := filepath.Join(e.dir, "icon.png")
	if err := os.WriteFile(icon, mustPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := notify.Send(notify.Notification{
		ID: "n1", Title: "Build done", Body: "3 warnings", Icon: icon,
		Urgency: notify.Critical, Timeout: -1,
	}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the notification to be displayed", func() bool { return e.displayed(t) == 1 })
	e.ctl(t, "close")
	waitClosed(t, e, "dunstctl close")
	h := e.history(t)
	if len(h) != 1 {
		t.Fatalf("history has %d entries: %v", len(h), h)
	}
	n := h[0]
	if n["summary"] != "Build done" || n["body"] != "3 warnings" {
		t.Fatalf("text: %v", n)
	}
	if n["urgency"] != "CRITICAL" {
		t.Fatalf("urgency %v", n["urgency"])
	}
	// A file path goes out as a file URI and dunst resolves it to the file.
	if n["icon_path"] != icon {
		t.Fatalf("icon_path %v, want %s", n["icon_path"], icon)
	}
	if n["appname"] != "dev.krate.sdkui" {
		t.Fatalf("appname %v", n["appname"])
	}
}

func TestRealDunstUrgencyLevels(t *testing.T) {
	e := startDunst(t)
	for i, u := range []notify.Urgency{notify.Low, notify.Normal, notify.Critical} {
		if err := notify.Send(notify.Notification{Title: "u" + string(rune('0'+i)), Urgency: u, Timeout: -1}); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, "three notifications", func() bool { return e.displayed(t) == 3 })
	e.ctl(t, "close-all")
	eventually(t, "history", func() bool { return len(e.history(t)) == 3 })
	got := map[string]any{}
	for _, n := range e.history(t) {
		got[n["summary"].(string)] = n["urgency"]
	}
	want := map[string]any{"u0": "LOW", "u1": "NORMAL", "u2": "CRITICAL"}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("urgencies %v, want %v", got, want)
		}
	}
}

func TestRealDunstReplaceByID(t *testing.T) {
	e := startDunst(t)
	for _, title := range []string{"first", "second", "third"} {
		if err := notify.Send(notify.Notification{ID: "same", Title: title, Body: "b-" + title, Timeout: -1}); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, "one displayed notification", func() bool { return e.displayed(t) == 1 })
	time.Sleep(200 * time.Millisecond)
	if n := e.displayed(t); n != 1 {
		t.Fatalf("replacing by ID left %d notifications displayed", n)
	}
	if err := notify.Send(notify.Notification{ID: "other", Title: "other", Timeout: -1}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "a second notification for a second ID", func() bool { return e.displayed(t) == 2 })
	e.ctl(t, "close-all")
	eventually(t, "history", func() bool { return len(e.history(t)) >= 2 })
	var titles []string
	for _, n := range e.history(t) {
		titles = append(titles, n["summary"].(string))
	}
	joined := strings.Join(titles, ",")
	if !strings.Contains(joined, "third") || strings.Contains(joined, "first") || strings.Contains(joined, "second") {
		t.Fatalf("history after replacement: %v (only the last text should remain)", titles)
	}
}

func TestRealDunstClosedByApp(t *testing.T) {
	e := startDunst(t)
	events := subscribeNotify(t)
	if err := notify.Send(notify.Notification{ID: "x", Title: "closing", Timeout: -1}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "displayed", func() bool { return e.displayed(t) == 1 })
	if err := notify.Close("x"); err != nil {
		t.Fatal(err)
	}
	c := waitClosed(t, e, "notify.Close")
	if c[1] != 3 {
		t.Fatalf("close reason %d, want 3 (closed by CloseNotification)", c[1])
	}
	if ev := waitEvent(t, events, "app close"); ev.ID != "x" || !ev.Closed {
		t.Fatalf("event %+v", ev)
	}
	eventually(t, "removed", func() bool { return e.displayed(t) == 0 })
	// Closing an unknown or already closed id is not an error.
	if err := notify.Close("x"); err != nil {
		t.Fatal(err)
	}
	if err := notify.Close("never-sent"); err != nil {
		t.Fatal(err)
	}
}

func TestRealDunstClosedByDaemon(t *testing.T) {
	e := startDunst(t)
	events := subscribeNotify(t)
	if err := notify.Send(notify.Notification{ID: "d", Title: "dismiss me", Timeout: -1}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "displayed", func() bool { return e.displayed(t) == 1 })
	e.ctl(t, "close")
	c := waitClosed(t, e, "dunstctl close")
	if c[1] != 2 {
		t.Fatalf("close reason %d, want 2 (dismissed by the user)", c[1])
	}
	if ev := waitEvent(t, events, "daemon close"); ev.ID != "d" || !ev.Closed || ev.Action != "" {
		t.Fatalf("event %+v", ev)
	}
}

func TestRealDunstExpires(t *testing.T) {
	e := startDunst(t)
	events := subscribeNotify(t)
	if err := notify.Send(notify.Notification{ID: "e", Title: "short lived", Timeout: 700 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	c := waitClosed(t, e, "expiry")
	if c[1] != 1 {
		t.Fatalf("close reason %d, want 1 (expired)", c[1])
	}
	if ev := waitEvent(t, events, "expiry"); ev.ID != "e" || !ev.Closed {
		t.Fatalf("event %+v", ev)
	}
}

func TestRealDunstDefaultAction(t *testing.T) {
	e := startDunst(t)
	events := subscribeNotify(t)
	if err := notify.Send(notify.Notification{
		ID: "a", Title: "act", Timeout: -1,
		Actions: []notify.Action{{ID: "open", Label: "Open"}, {ID: "later", Label: "Later"}},
	}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "displayed", func() bool { return e.displayed(t) == 1 })
	// "dunstctl action" runs the default action: a click on the body.
	e.ctl(t, "action", "0")
	ev := waitEvent(t, events, "default action")
	if ev.ID != "a" || ev.Action != "" || ev.Closed {
		t.Fatalf("event %+v, want a click on notification a", ev)
	}
}

func TestRealDunstActionButton(t *testing.T) {
	e := startDunst(t)
	events := subscribeNotify(t)
	if err := notify.Send(notify.Notification{
		ID: "b", Title: "buttons", Timeout: -1,
		Actions: []notify.Action{{ID: "open", Label: "Open it"}, {ID: "later", Label: "Remind me"}},
	}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "displayed", func() bool { return e.displayed(t) == 1 })
	// The context menu lists the actions; the test's dmenu script picks one.
	e.setPick(t, "Remind me")
	e.ctl(t, "context")
	ev := waitEvent(t, events, "context menu action")
	menu, _ := os.ReadFile(filepath.Join(e.dir, "menu.txt"))
	if !strings.Contains(string(menu), "Open it") || !strings.Contains(string(menu), "Remind me") {
		t.Fatalf("dunst's menu did not list both actions: %q", menu)
	}
	if ev.ID != "b" || ev.Action != "later" {
		t.Fatalf("event %+v, want action later on b", ev)
	}
	// An action is not followed by a second "closed" event for the same
	// notification.
	select {
	case extra := <-events:
		if extra.Closed {
			t.Fatalf("closed reported after an action: %+v", extra)
		}
	case <-time.After(500 * time.Millisecond):
	}
}

func TestRealDunstGoneIsUnsupported(t *testing.T) {
	e := startDunst(t)
	if err := notify.Send(notify.Notification{Title: "while running", Timeout: -1}); err != nil {
		t.Fatal(err)
	}
	e.stop()
	eventually(t, "the name to be released", func() bool { return !sessionbus.NameHasOwner("org.freedesktop.Notifications") })
	err := notify.Send(notify.Notification{Title: "no daemon"})
	if !krate.IsUnsupported(err) {
		t.Fatalf("send without a daemon: %v", err)
	}
	if host.SupportsForTest("notify") {
		t.Fatal("notify still reported as supported")
	}
	// Close with nothing to close stays quiet.
	if err := notify.Close("anything"); err != nil {
		t.Fatal(err)
	}
}

func TestRealNoDaemonOnARealBus(t *testing.T) {
	need(t)
	privateBus(t)
	if host.SupportsForTest("notify") {
		t.Fatal("notify reported as supported on a bus with no daemon")
	}
	err := notify.Send(notify.Notification{Title: "x"})
	if !krate.IsUnsupported(err) || !strings.Contains(err.Error(), "no notification service") {
		t.Fatalf("err = %v", err)
	}
}

func mustPNG() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		panic(err)
	}
	return b.Bytes()
}
