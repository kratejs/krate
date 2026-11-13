package media

import (
	"sync/atomic"
	"testing"

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
