// Package features detects which optional webview engine features a native
// app's UI uses, so the memory profiles can switch the others off.
//
// Detection is textual and deliberately generous: a pattern found anywhere in
// the built UI (HTML and JavaScript) turns the feature on, because a missed
// feature breaks the app while a spurious one only costs memory.
package features

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Set records which features the UI uses.
type Set struct {
	Media      bool `json:"media"`
	WebAudio   bool `json:"webaudio"`
	WebGL      bool `json:"webgl"`
	WebRTC     bool `json:"webrtc"`
	SpellCheck bool `json:"spellcheck"`
}

// Names lists the feature names in a fixed order.
var Names = []string{"media", "webaudio", "webgl", "webrtc", "spellcheck"}

// Get reports a feature by name.
func (s Set) Get(name string) bool {
	switch name {
	case "media":
		return s.Media
	case "webaudio":
		return s.WebAudio
	case "webgl":
		return s.WebGL
	case "webrtc":
		return s.WebRTC
	case "spellcheck":
		return s.SpellCheck
	}
	return false
}

// Merge returns the union of two sets.
func (s Set) Merge(o Set) Set {
	return Set{
		Media: s.Media || o.Media, WebAudio: s.WebAudio || o.WebAudio, WebGL: s.WebGL || o.WebGL,
		WebRTC: s.WebRTC || o.WebRTC, SpellCheck: s.SpellCheck || o.SpellCheck,
	}
}

// Enabled lists the names of the features that are on.
func (s Set) Enabled() []string {
	var out []string
	for _, n := range Names {
		if s.Get(n) {
			out = append(out, n)
		}
	}
	return out
}

// Override holds the explicit native.features values; nil means detect.
type Override struct {
	Media, WebAudio, WebGL, WebRTC, SpellCheck *bool
}

// Resolve applies explicit overrides on top of the detected set.
func (s Set) Resolve(o Override) Set {
	pick := func(det bool, ov *bool) bool {
		if ov != nil {
			return *ov
		}
		return det
	}
	return Set{
		Media: pick(s.Media, o.Media), WebAudio: pick(s.WebAudio, o.WebAudio), WebGL: pick(s.WebGL, o.WebGL),
		WebRTC: pick(s.WebRTC, o.WebRTC), SpellCheck: pick(s.SpellCheck, o.SpellCheck),
	}
}

var (
	reMedia = regexp.MustCompile(`(?i)<(?:video|audio)[\s>/]|\bnew\s+Audio\s*\(|\bMediaSource\b|\bHTML(?:Media|Video|Audio)Element\b|createElement(?:NS)?\(\s*(?:[^,)]*,\s*)?["'` + "`" + `](?:video|audio)["'` + "`" + `]`)
	reAudio = regexp.MustCompile(`\b(?:Offline|webkit)?AudioContext\b`)
	reGL    = regexp.MustCompile(`getContext\(\s*["'` + "`" + `](?:webgl2?|experimental-webgl)["'` + "`" + `]`)
	reRTC   = regexp.MustCompile(`\bRTCPeerConnection\b|\bgetUserMedia\b|\bgetDisplayMedia\b|\bwebkitRTCPeerConnection\b`)
	reCE    = regexp.MustCompile(`(?i)\bcontent-?editable\b`)
	reSpell = regexp.MustCompile(`(?i)\bspell-?check\b`)
)

// Scan reports the features that one source text uses.
func Scan(src string) Set {
	var s Set
	s.Media = reMedia.MatchString(src)
	s.WebAudio = reAudio.MatchString(src)
	s.WebGL = reGL.MatchString(src)
	s.WebRTC = reRTC.MatchString(src)
	s.SpellCheck = reCE.MatchString(src)
	if !s.SpellCheck {
		for _, m := range reSpell.FindAllStringIndex(src, -1) {
			rest := src[m[1]:]
			if len(rest) > 24 {
				rest = rest[:24]
			}
			rest = strings.TrimLeft(rest, " :=\"'{")
			if !strings.HasPrefix(rest, "false") && !strings.HasPrefix(rest, "!1") && !strings.HasPrefix(rest, "0") {
				s.SpellCheck = true
				break
			}
		}
	}
	// A page that captures media or streams it needs the media machinery too.
	if s.WebRTC {
		s.Media = true
	}
	return s
}

const maxFile = 8 << 20

// ScanDir scans the HTML and JavaScript files below dir (the built UI).
// Files over 8 MiB, dot directories and source maps are skipped.
func ScanDir(dir string) (Set, error) {
	var out Set
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".html", ".htm", ".js", ".mjs", ".cjs":
		default:
			return nil
		}
		if info, err := d.Info(); err != nil || info.Size() > maxFile {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		out = out.Merge(Scan(string(b)))
		return nil
	})
	return out, err
}

// Uses records which webview permissions the UI code asks for. They decide
// the "media" and "location" app permissions of a derived native build and
// the usage descriptions packaging needs.
type Uses struct {
	// Capture is getUserMedia (camera or microphone) or getDisplayMedia.
	Capture bool `json:"capture"`
	// Camera and Microphone narrow Capture when the constraints are literal:
	// both stay true when they cannot be told apart.
	Camera     bool `json:"camera"`
	Microphone bool `json:"microphone"`
	Screen     bool `json:"screen"`
	// Location is navigator.geolocation.
	Location bool `json:"location"`
	// File and Line are where the first use was found, relative to the
	// scanned directory, for diagnostics.
	File string `json:"-"`
	Line int    `json:"-"`
}

// Any reports whether the code uses any of them.
func (u Uses) Any() bool { return u.Capture || u.Screen || u.Location }

var (
	reGUM     = regexp.MustCompile(`\bgetUserMedia\b`)
	reDisplay = regexp.MustCompile(`\bgetDisplayMedia\b`)
	reGeo     = regexp.MustCompile(`\bgeolocation\b`)
	// A literal "video: false" or "audio: false" next to a getUserMedia call
	// narrows it.
	reVideoOff = regexp.MustCompile(`\bvideo\s*:\s*(?:false|!1|0)\b`)
	reAudioOff = regexp.MustCompile(`\baudio\s*:\s*(?:false|!1|0)\b`)
	reVideoOn  = regexp.MustCompile(`\bvideo\s*:`)
	reAudioOn  = regexp.MustCompile(`\baudio\s*:`)
)

// ScanUses reports the permissions one source text asks the webview for.
// Like Scan it is textual and generous: a mention counts.
func ScanUses(src string) Uses {
	var u Uses
	if loc := reGUM.FindStringIndex(src); loc != nil {
		u.Capture = true
		// Look at the constraints near each call.
		for _, m := range reGUM.FindAllStringIndex(src, -1) {
			end := m[1] + 160
			if end > len(src) {
				end = len(src)
			}
			win := src[m[1]:end]
			vOff, aOff := reVideoOff.MatchString(win), reAudioOff.MatchString(win)
			vOn, aOn := reVideoOn.MatchString(win), reAudioOn.MatchString(win)
			if (!vOn && !aOn) || (vOn && !vOff) {
				u.Camera = true
			}
			if (!vOn && !aOn) || (aOn && !aOff) {
				u.Microphone = true
			}
		}
	}
	if reDisplay.MatchString(src) {
		u.Screen = true
	}
	if reGeo.MatchString(src) && strings.Contains(src, "navigator") {
		u.Location = true
	}
	return u
}

// ScanDirUses scans the HTML and JavaScript files below dir.
func ScanDirUses(dir string) (Uses, error) {
	var out Uses
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".html", ".htm", ".js", ".mjs", ".cjs":
		default:
			return nil
		}
		if info, err := d.Info(); err != nil || info.Size() > maxFile {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		u := ScanUses(string(b))
		if !u.Any() {
			return nil
		}
		if !out.Any() {
			rel, _ := filepath.Rel(dir, p)
			out.File = filepath.ToSlash(rel)
			re := reGUM
			switch {
			case u.Capture:
			case u.Screen:
				re = reDisplay
			default:
				re = reGeo
			}
			if loc := re.FindStringIndex(string(b)); loc != nil {
				out.Line = 1 + strings.Count(string(b)[:loc[0]], "\n")
			}
		}
		out.Capture = out.Capture || u.Capture
		out.Camera = out.Camera || u.Camera
		out.Microphone = out.Microphone || u.Microphone
		out.Screen = out.Screen || u.Screen
		out.Location = out.Location || u.Location
		return nil
	})
	return out, err
}
