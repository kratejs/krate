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
	// The urgency is not in dunstctl history before dunst 1.10 (Ubuntu 24.04
	// ships 1.9.2), so a rule script records what dunst parsed from each hint.
	writeFile(t, filepath.Join(dir, "shown.sh"), "#!/bin/sh\nprintf '%s\\t%s\\n' \"$DUNST_SUMMARY\" \"$DUNST_URGENCY\" >> \""+dir+"/shown.txt\"\n", 0o755)
	writeFile(t, filepath.Join(dir, "dunstrc"), "[global]\n    dmenu = "+filepath.Join(dir, "pick.sh")+"\n    follow = none\n    sticky_history = yes\n\n[record]\n    summary = \"*\"\n    script = "+filepath.Join(dir, "shown.sh")+"\n", 0o644)
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

// shownUrgencies maps each notification summary dunst displayed to the
// urgency it parsed (LOW, NORMAL or CRITICAL), as the rule script saw it.
func (e *dunstEnv) shownUrgencies() map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(filepath.Join(e.dir, "shown.txt"))
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(data), "\n") {
		if k, v, ok := strings.Cut(l, "\t"); ok {
			out[k] = v
		}
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
	eventually(t, "dunst to report the urgency", func() bool { return e.shownUrgencies()["Build done"] == "CRITICAL" })
	// A file path goes out as a file URI and dunst resolves it to the file.
	if n["icon_path"] != icon {
		t.Fatalf("icon_path %v, want %s", n["icon_path"], icon)
	}
	if n["appname"] != "dev.krate.sdkui" {
		t.Fatalf("appname %v", n["appname"])
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
