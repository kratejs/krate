package features

import (
	"testing"
)

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
