package features

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan(t *testing.T) {
	cases := []struct {
		name, src string
		want      Set
	}{
		{"none", `<div class="a">hi</div><script>console.log(1)</script>`, Set{}},
		{"video tag", `<main><video src="a.mp4"></video></main>`, Set{Media: true}},
		{"audio tag", `<audio controls>`, Set{Media: true}},
		{"new Audio", `const a = new Audio("x.mp3"); a.play()`, Set{Media: true}},
		{"MediaSource", `new MediaSource()`, Set{Media: true}},
		{"createElement video", `document.createElement("video")`, Set{Media: true}},
		{"jsx runtime audio", "h('audio', {src})", Set{}},
		{"audio context", `new AudioContext()`, Set{WebAudio: true}},
		{"offline", `new OfflineAudioContext(1, 1, 8000)`, Set{WebAudio: true}},
		{"webkit audio", `window.webkitAudioContext`, Set{WebAudio: true}},
		{"webgl", `c.getContext("webgl")`, Set{WebGL: true}},
		{"webgl2", `c.getContext('webgl2', {})`, Set{WebGL: true}},
		{"experimental", "c.getContext(`experimental-webgl`)", Set{WebGL: true}},
		{"2d", `c.getContext("2d")`, Set{}},
		{"rtc", `new RTCPeerConnection()`, Set{WebRTC: true, Media: true}},
		{"gum", `navigator.mediaDevices.getUserMedia({audio:true})`, Set{WebRTC: true, Media: true}},
		{"contenteditable", `<div contenteditable="true">`, Set{SpellCheck: true}},
		{"jsx contentEditable", `<div contentEditable>`, Set{SpellCheck: true}},
		{"spellcheck true", `<textarea spellcheck="true">`, Set{SpellCheck: true}},
		{"spellCheck jsx", `<input spellCheck={true} />`, Set{SpellCheck: true}},
		{"spellcheck bare", `<input spellcheck>`, Set{SpellCheck: true}},
		{"spellcheck false", `<input spellcheck="false">`, Set{}},
		{"spellCheck false jsx", `<input spellCheck={false} />`, Set{}},
		{"spellCheck minified false", `{spellCheck:!1}`, Set{}},
	}
	for _, c := range cases {
		if got := Scan(c.src); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestResolve(t *testing.T) {
	f, tr := false, true
	got := Set{Media: true, WebGL: true}.Resolve(Override{Media: &f, WebAudio: &tr})
	if got.Media || !got.WebAudio || !got.WebGL || got.WebRTC {
		t.Fatalf("got %+v", got)
	}
}

func TestScanDir(t *testing.T) {
	dir := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, "a", ".krate"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<p>x</p>`), 0o644))
	must(os.WriteFile(filepath.Join(dir, "a", "app.js"), []byte(`c.getContext("webgl2")`), 0o644))
	must(os.WriteFile(filepath.Join(dir, "a", ".krate", "x.js"), []byte(`new AudioContext()`), 0o644))
	must(os.WriteFile(filepath.Join(dir, "a", "app.js.map"), []byte(`new Audio(`), 0o644))
	got, err := ScanDir(dir)
	must(err)
	if got != (Set{WebGL: true}) {
		t.Fatalf("got %+v", got)
	}
	if n := got.Enabled(); len(n) != 1 || n[0] != "webgl" {
		t.Fatalf("enabled %v", n)
	}
}

func TestScanUses(t *testing.T) {
	cases := []struct {
		name, src string
		want      Uses
	}{
		{"none", `fetch("/x")`, Uses{}},
		{"camera only", `navigator.mediaDevices.getUserMedia({ video: true })`, Uses{Capture: true, Camera: true}},
		{"mic only", `navigator.mediaDevices.getUserMedia({ audio: true, video: false })`, Uses{Capture: true, Microphone: true}},
		{"both", `getUserMedia({audio:true,video:{width:640}})`, Uses{Capture: true, Camera: true, Microphone: true}},
		{"unknown constraints", `getUserMedia(constraints)`, Uses{Capture: true, Camera: true, Microphone: true}},
		{"screen", `navigator.mediaDevices.getDisplayMedia({video:true})`, Uses{Screen: true}},
		{"geolocation", `navigator.geolocation.getCurrentPosition(f)`, Uses{Location: true}},
		{"a bare word is not a use", `const geolocation = 1`, Uses{}},
	}
	for _, c := range cases {
		got := ScanUses(c.src)
		if got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestScanDirUsesReportsFirstSite(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.js", "var x = 1;\nvar y = 2;\nnavigator.mediaDevices.getUserMedia({audio: true});\n")
	write("b.html", "<p>no</p>")
	write("c.map", "getUserMedia")
	u, err := ScanDirUses(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !u.Capture || !u.Microphone || u.Camera || u.File != "a.js" || u.Line != 3 {
		t.Fatalf("%+v", u)
	}
}
