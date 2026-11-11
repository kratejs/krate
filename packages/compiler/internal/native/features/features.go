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
