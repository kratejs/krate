package media

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

type fakePrivacy struct {
	status  map[caps.PermissionKind]caps.PrivacyStatus
	asked   []caps.PermissionKind
	onAsk   caps.PrivacyStatus
	askFail error
}

func (f *fakePrivacy) PrivacyStatus(k caps.PermissionKind) (caps.PrivacyStatus, error) {
	if s, ok := f.status[k]; ok {
		return s, nil
	}
	return caps.PrivacyUnknown, nil
}

func (f *fakePrivacy) RequestPrivacy(k caps.PermissionKind) (caps.PrivacyStatus, error) {
	f.asked = append(f.asked, k)
	if f.askFail != nil {
		return "", f.askFail
	}
	f.status[k] = f.onAsk
	return f.onAsk, nil
}

func allowAll(view, perm string) bool { return true }

func baseHost() Host {
	return Host{
		ViewID: func(r caps.ViewRef) (string, bool) {
			if r == 0 {
				return "", false
			}
			return map[caps.ViewRef]string{1: "main", 2: "v2"}[r], true
		},
		Allows: allowAll,
	}
}

func req(id uint64, view caps.ViewRef, kinds ...Kind) caps.PermissionRequest {
	return caps.PermissionRequest{ID: id, View: view, Origin: "krate://app", Kinds: kinds}
}

func TestRequiredPermission(t *testing.T) {
	for k, want := range map[Kind]string{
		caps.PermCamera: "media", caps.PermMicrophone: "media", caps.PermScreen: "media",
		caps.PermLocation: "location", caps.PermNotifications: "notify", caps.PermClipboardRead: "clipboard:read",
	} {
		if got, ok := RequiredPermission(k); !ok || got != want {
			t.Errorf("%s: %q %v", k, got, ok)
		}
	}
	if _, ok := RequiredPermission(caps.PermOther); ok {
		t.Error("other must have no permission")
	}
}

func TestScopeWithoutPermissionDenies(t *testing.T) {
	h := baseHost()
	h.Allows = func(view, perm string) bool { return !(view == "v2" && perm == "media") }
	m := NewManager(Config{Policy: map[string]string{"camera": "allow", "location": "allow"}}, h)
	if o := m.Decide(req(1, 2, caps.PermCamera)); o.Allowed || o.Reason != "scope" {
		t.Fatalf("v2 camera: %+v", o)
	}
	if o := m.Decide(req(2, 1, caps.PermCamera)); !o.Allowed {
		t.Fatalf("main camera: %+v", o)
	}
	// One kind without a permission denies the whole request.
	h.Allows = func(view, perm string) bool { return perm != "location" }
	m = NewManager(Config{Policy: map[string]string{"camera": "allow", "location": "allow"}}, h)
	if o := m.Decide(req(3, 1, caps.PermCamera, caps.PermLocation)); o.Allowed || o.Reason != "scope" {
		t.Fatalf("camera and location: %+v", o)
	}
}

func TestNoPermissionCheckDeniesEverything(t *testing.T) {
	h := baseHost()
	h.Allows = nil
	m := NewManager(Config{Policy: map[string]string{"camera": "allow"}}, h)
	if o := m.Decide(req(1, 1, caps.PermCamera)); o.Allowed {
		t.Fatal("a missing permission check must deny")
	}
}

func TestUnknownViewAndKinds(t *testing.T) {
	m := NewManager(Config{Policy: map[string]string{"camera": "allow"}}, baseHost())
	if o := m.Decide(req(1, 0, caps.PermCamera)); o.Allowed || o.Reason != "unknown view" {
		t.Fatalf("%+v", o)
	}
	if o := m.Decide(req(2, 1)); o.Allowed {
		t.Fatalf("no kinds: %+v", o)
	}
	if o := m.Decide(req(3, 1, caps.PermOther)); o.Allowed || o.Reason != "unsupported" {
		t.Fatalf("other: %+v", o)
	}
}

func TestPolicyAllowDenyAndDefault(t *testing.T) {
	m := NewManager(Config{Policy: map[string]string{"camera": "allow", "microphone": "deny", "bogus": "allow", "location": "maybe"}}, baseHost())
	if o := m.Decide(req(1, 1, caps.PermCamera)); !o.Allowed || o.Reason != "policy" {
		t.Fatalf("camera: %+v", o)
	}
	if o := m.Decide(req(2, 1, caps.PermMicrophone)); o.Allowed || o.Reason != "policy" {
		t.Fatalf("microphone: %+v", o)
	}
	// Camera allowed, microphone denied: the request is denied.
	if o := m.Decide(req(3, 1, caps.PermCamera, caps.PermMicrophone)); o.Allowed {
		t.Fatalf("both: %+v", o)
	}
	// A bad policy value and an unknown kind are ignored, so location prompts;
	// without a prompt UI that is a denial.
	pol := m.Policy()
	if pol[caps.PermLocation] != Prompt || len(pol) != len(Kinds) {
		t.Fatalf("policy %v", pol)
	}
	if o := m.Decide(req(4, 1, caps.PermLocation)); o.Allowed || o.Reason != "no handler" {
		t.Fatalf("location: %+v", o)
	}
	if err := m.SetPolicy(caps.PermLocation, Allow); err != nil {
		t.Fatal(err)
	}
	if o := m.Decide(req(5, 1, caps.PermLocation)); !o.Allowed {
		t.Fatalf("location after SetPolicy: %+v", o)
	}
	if m.SetPolicy("nope", Allow) == nil || m.SetPolicy(caps.PermCamera, "sometimes") == nil {
		t.Fatal("bad SetPolicy arguments must fail")
	}
}

func TestPromptRemembersAnswers(t *testing.T) {
	var asked int32
	var last Request
	h := baseHost()
	answerYes := true
	h.Prompt = func(r Request) (bool, bool) { atomic.AddInt32(&asked, 1); last = r; return answerYes, true }
	m := NewManager(Config{}, h)
	if o := m.Decide(req(1, 1, caps.PermCamera, caps.PermMicrophone)); !o.Allowed || o.Reason != "prompt" {
		t.Fatalf("%+v", o)
	}
	if len(last.Kinds) != 2 || last.View != "main" {
		t.Fatalf("prompt saw %+v", last)
	}
	// Remembered: no second prompt, for either kind alone.
	for i, k := range []Kind{caps.PermCamera, caps.PermMicrophone} {
		if o := m.Decide(req(uint64(10+i), 1, k)); !o.Allowed || o.Reason != "remembered" {
			t.Fatalf("%s: %+v", k, o)
		}
	}
	if asked != 1 {
		t.Fatalf("prompted %d times", asked)
	}
	// A new kind prompts again but only for the new kind.
	answerYes = false
	if o := m.Decide(req(20, 1, caps.PermCamera, caps.PermLocation)); o.Allowed {
		t.Fatalf("%+v", o)
	}
	if len(last.Kinds) != 1 || last.Kinds[0] != caps.PermLocation {
		t.Fatalf("second prompt asked about %v", last.Kinds)
	}
	// The denial is remembered too.
	if o := m.Decide(req(21, 1, caps.PermLocation)); o.Allowed || o.Reason != "remembered" {
		t.Fatalf("%+v", o)
	}
	ds := m.Decisions()
	if len(ds) != 3 {
		t.Fatalf("decisions %+v", ds)
	}
	m.Reset(caps.PermLocation)
	if len(m.Decisions()) != 2 {
		t.Fatalf("reset one: %+v", m.Decisions())
	}
	m.Reset("")
	if len(m.Decisions()) != 0 {
		t.Fatalf("reset all: %+v", m.Decisions())
	}
	if o := m.Decide(req(22, 1, caps.PermCamera)); o.Reason != "prompt" {
		t.Fatalf("after reset: %+v", o)
	}
}

func TestNoRememberAsksEveryTime(t *testing.T) {
	var asked int
	h := baseHost()
	h.Prompt = func(Request) (bool, bool) { asked++; return true, true }
	m := NewManager(Config{NoRemember: true}, h)
	m.Decide(req(1, 1, caps.PermCamera))
	m.Decide(req(2, 1, caps.PermCamera))
	if asked != 2 || len(m.Decisions()) != 0 {
		t.Fatalf("asked %d, decisions %v", asked, m.Decisions())
	}
}

func TestViewScopedDecisionWinsOverApp(t *testing.T) {
	m := NewManager(Config{Policy: map[string]string{"camera": "deny"}}, baseHost())
	if err := m.Remember(caps.PermCamera, "app", true); err != nil {
		t.Fatal(err)
	}
	if err := m.Remember(caps.PermCamera, "v2", false); err != nil {
		t.Fatal(err)
	}
	if o := m.Decide(req(1, 1, caps.PermCamera)); !o.Allowed || o.Reason != "remembered" {
		t.Fatalf("main: %+v", o)
	}
	if o := m.Decide(req(2, 2, caps.PermCamera)); o.Allowed || o.Reason != "remembered" {
		t.Fatalf("v2: %+v", o)
	}
	ds := m.Decisions()
	if len(ds) != 2 || ds[0].Scope != "app" || ds[1].Scope != "v2" {
		t.Fatalf("decisions %+v", ds)
	}
	m.ForgetView("v2")
	if o := m.Decide(req(3, 2, caps.PermCamera)); !o.Allowed {
		t.Fatalf("after ForgetView, the app decision applies: %+v", o)
	}
}

func TestStorePersistsAppDecisionsOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "permissions.json")
	m := NewManager(Config{StorePath: path}, baseHost())
	m.Remember(caps.PermCamera, "app", true)
	m.Remember(caps.PermMicrophone, "v2", true)
	m.Remember(caps.PermLocation, "app", false)
	m2 := NewManager(Config{StorePath: path}, baseHost())
	ds := m2.Decisions()
	if len(ds) != 2 || ds[0].Kind != caps.PermCamera || !ds[0].Allow || ds[1].Kind != caps.PermLocation || ds[1].Allow {
		t.Fatalf("reloaded %+v", ds)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("store file: %v %v", info, err)
	}
	m2.Reset("")
	if len(NewManager(Config{StorePath: path}, baseHost()).Decisions()) != 0 {
		t.Fatal("reset must be stored")
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	m3 := NewManager(Config{StorePath: path}, baseHost())
	if m3.LoadError() == nil || len(m3.Decisions()) != 0 {
		t.Fatalf("a corrupt store must be reported and ignored: %v", m3.LoadError())
	}
}

func TestHandlerStreamDecides(t *testing.T) {
	var m *Manager
	pubs := make(chan Request, 4)
	h := baseHost()
	h.Handlers = func() int { return 1 }
	h.Publish = func(r Request) { pubs <- r }
	m = NewManager(Config{Policy: map[string]string{"camera": "deny"}}, h)

	go func() {
		r := <-pubs
		m.Respond(r.ID, true, "view")
	}()
	o := m.Decide(req(7, 2, caps.PermCamera))
	if !o.Allowed || o.Reason != "handler" {
		t.Fatalf("%+v", o)
	}
	// "view" remembers for that view only.
	ds := m.Decisions()
	if len(ds) != 1 || ds[0].Scope != "v2" {
		t.Fatalf("decisions %+v", ds)
	}
	if o := m.Decide(req(8, 2, caps.PermCamera)); o.Reason != "remembered" || !o.Allowed {
		t.Fatalf("%+v", o)
	}

	// A handler that says no, without remembering.
	go func() {
		r := <-pubs
		m.Respond(r.ID, false, "none")
	}()
	if o := m.Decide(req(9, 1, caps.PermMicrophone)); o.Allowed || o.Reason != "handler" {
		t.Fatalf("%+v", o)
	}
	if len(m.Decisions()) != 1 {
		t.Fatalf("none must not remember: %+v", m.Decisions())
	}
	if m.Respond(9, true, "") == nil {
		t.Fatal("a second answer must fail")
	}
	if m.Respond(99, true, "bogus") == nil {
		t.Fatal("a bad remember value must fail")
	}
}

func TestHandlerTimeoutDenies(t *testing.T) {
	h := baseHost()
	h.Handlers = func() int { return 1 }
	h.Publish = func(Request) {}
	h.HandlerTimeout = 30 * time.Millisecond
	m := NewManager(Config{Policy: map[string]string{"camera": "allow"}}, h)
	if o := m.Decide(req(1, 1, caps.PermCamera)); o.Allowed || o.Reason != "timeout" {
		t.Fatalf("%+v", o)
	}
}

func TestRememberedDecisionSkipsHandler(t *testing.T) {
	h := baseHost()
	h.Handlers = func() int { return 1 }
	h.Publish = func(Request) { t.Error("a remembered kind must not reach the handler") }
	m := NewManager(Config{}, h)
	m.Remember(caps.PermCamera, "app", true)
	if o := m.Decide(req(1, 1, caps.PermCamera)); !o.Allowed {
		t.Fatalf("%+v", o)
	}
}

func TestOSConsent(t *testing.T) {
	h := baseHost()
	fp := &fakePrivacy{status: map[caps.PermissionKind]caps.PrivacyStatus{
		caps.PermCamera: caps.PrivacyDenied, caps.PermMicrophone: caps.PrivacyNotDetermined, caps.PermLocation: caps.PrivacyRestricted,
	}, onAsk: caps.PrivacyGranted}
	h.Privacy = fp
	m := NewManager(Config{Policy: map[string]string{"camera": "allow", "microphone": "allow", "location": "allow", "screen": "allow"}}, h)
	if o := m.Decide(req(1, 1, caps.PermCamera)); o.Allowed || o.Reason != "os" {
		t.Fatalf("OS-denied camera: %+v", o)
	}
	if o := m.Decide(req(2, 1, caps.PermLocation)); o.Allowed {
		t.Fatalf("restricted location: %+v", o)
	}
	if o := m.Decide(req(3, 1, caps.PermMicrophone)); !o.Allowed {
		t.Fatalf("microphone after the OS prompt: %+v", o)
	}
	if len(fp.asked) != 1 || fp.asked[0] != caps.PermMicrophone {
		t.Fatalf("OS prompts: %v", fp.asked)
	}
	// Unknown means there is no switch: allowed.
	if o := m.Decide(req(4, 1, caps.PermScreen)); !o.Allowed {
		t.Fatalf("screen: %+v", o)
	}
	// A refused OS prompt denies.
	fp.status[caps.PermMicrophone] = caps.PrivacyNotDetermined
	fp.onAsk = caps.PrivacyDenied
	if o := m.Decide(req(5, 1, caps.PermMicrophone)); o.Allowed {
		t.Fatalf("microphone refused by the OS prompt: %+v", o)
	}
}

func TestHandleAnswersOnce(t *testing.T) {
	m := NewManager(Config{Policy: map[string]string{"camera": "allow"}}, baseHost())
	got := make(chan bool, 2)
	m.Handle(req(1, 1, caps.PermCamera), func(b bool) { got <- b })
	select {
	case b := <-got:
		if !b {
			t.Fatal("denied")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no answer")
	}
}
