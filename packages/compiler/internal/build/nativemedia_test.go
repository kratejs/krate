package build

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native/capability"
	"github.com/kratejs/krate/packages/compiler/internal/native/features"
)

func TestDevUsePermissions(t *testing.T) {
	camera := features.Uses{Capture: true, Camera: true, File: "app.js", Line: 3}
	got, notes := devUsePermissions(nil, camera)
	if len(notes) != 0 || !hasName(got, "media") || !hasName(got, "kv") || !hasName(got, "window") {
		t.Fatalf("defaults plus media expected, got %v %v", got, notes)
	}
	got, _ = devUsePermissions(nil, features.Uses{Location: true})
	if !hasName(got, "location") || hasName(got, "media") {
		t.Fatalf("%v", got)
	}
	if got, notes := devUsePermissions(nil, features.Uses{}); got != nil || notes != nil {
		t.Fatalf("an app that uses nothing keeps the defaults: %v %v", got, notes)
	}
	// An explicit list is kept; a missing name is a note, not a silent denial.
	got, notes = devUsePermissions([]string{"kv"}, camera)
	if len(got) != 1 || len(notes) != 1 || !strings.Contains(notes[0], `"media"`) || !strings.Contains(notes[0], "app.js") {
		t.Fatalf("%v %v", got, notes)
	}
	// media:manage implies media.
	if _, notes := devUsePermissions([]string{"media:manage"}, camera); len(notes) != 0 {
		t.Fatalf("media:manage covers media: %v", notes)
	}
}

func hasName(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestNativeMediaMeta(t *testing.T) {
	if nativeMediaMeta(&config.NativeConfig{}, features.Uses{}) != nil {
		t.Fatal("nothing configured, nothing used: no media block")
	}
	no := false
	m := nativeMediaMeta(&config.NativeConfig{Media: config.NativeMedia{Camera: "allow", Remember: &no}}, features.Uses{Capture: true, Camera: true, Location: true})
	if m == nil || m.Policy["camera"] != "allow" || !m.NoRemember || strings.Join(m.Uses, ",") != "camera,location" {
		t.Fatalf("%+v", m)
	}
}

func TestMediaUsageDiagnostics(t *testing.T) {
	u := features.Uses{Capture: true, Camera: true, Microphone: true, Location: true, File: "index.html", Line: 9}
	eff := &config.NativeConfig{}
	ds := mediaUsageDiagnostics(eff, u)
	if len(ds) != 3 {
		t.Fatalf("camera, microphone and location lack a sentence: %v", ds)
	}
	for _, d := range ds {
		if d.Code != diag.CodeNativeMediaUsage || d.File != "index.html" || d.Line != 9 {
			t.Errorf("%+v", d)
		}
	}
	eff.Media.Usage = config.NativeMediaUsage{Camera: "a", Microphone: "b", Location: "c"}
	if ds := mediaUsageDiagnostics(eff, u); len(ds) != 0 {
		t.Fatalf("all described: %v", ds)
	}
	if ds := mediaUsageDiagnostics(&config.NativeConfig{}, features.Uses{}); len(ds) != 0 {
		t.Fatalf("nothing used: %v", ds)
	}
}

func TestAddUsesJoinsTheDerivedPermissions(t *testing.T) {
	res := &capability.Result{Needs: map[string][]capability.Site{"kv": {{File: "a.ts"}}}, Permissions: []string{"kv"}}
	res.AddUses(features.Uses{Capture: true, Camera: true, Location: true, File: "ui.js", Line: 2})
	if strings.Join(res.Permissions, ",") != "kv,location,media" {
		t.Fatalf("%v", res.Permissions)
	}
	if s := res.Needs["media"]; len(s) != 1 || s[0].File != "ui.js" || s[0].Symbol != "getUserMedia" {
		t.Fatalf("%+v", s)
	}
	// The KR7210 check names the site when native.permissions lacks it.
	ds := permissionDiagnostics(res, []string{"kv"})
	if len(ds) != 2 {
		t.Fatalf("%v", ds)
	}
	if ds := permissionDiagnostics(res, []string{"kv", "media:manage", "location"}); len(ds) != 0 {
		t.Fatalf("media:manage covers media: %v", ds)
	}
	// Idempotent.
	res.AddUses(features.Uses{Capture: true, File: "ui.js", Line: 2})
	if len(res.Needs["media"]) != 1 {
		t.Fatalf("%+v", res.Needs["media"])
	}
}
