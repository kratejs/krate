// Package toastscheme holds the pure parts of Windows toast activation after
// an app has quit: the URL scheme a toast click launches, the arguments it
// carries and the registry keys and manifest entries that register the
// scheme. The Windows platform layer, the NSIS script and the MSIX manifest
// all derive the scheme from the app id here, so they always agree.
//
// A toast uses activationType "protocol": a click runs the registered handler
// of the scheme (the app executable with the URL as its argument) whether or
// not the app is running. A new process hands the click to a running instance
// or, when there is none, delivers it to the page as a notification event once
// the app has started.
package toastscheme

import (
	"crypto/sha1"
	"encoding/hex"
	"net/url"
	"strings"
)

const prefix = "krate-toast-"

// Scheme returns the URL scheme of an app: "krate-toast-" and 12 hex digits of
// a hash of the app id. Schemes are letters, digits, "+", "-" and ".", and
// start with a letter.
func Scheme(appID string) string {
	sum := sha1.Sum([]byte(strings.ToLower(appID)))
	return prefix + hex.EncodeToString(sum[:])[:12]
}

// IsScheme reports whether s is a scheme this package produced.
func IsScheme(s string) bool {
	if !strings.HasPrefix(s, prefix) || len(s) != len(prefix)+12 {
		return false
	}
	_, err := hex.DecodeString(s[len(prefix):])
	return err == nil
}

// Args encodes what a click returns: the notification id and the action id
// ("" for the body). Windows treats the string as opaque.
func Args(id, action string) string {
	v := url.Values{}
	v.Set("krate", "1")
	v.Set("id", id)
	if action != "" {
		v.Set("action", action)
	}
	return v.Encode()
}

// ParseArgs reverses Args. ok is false for arguments this package did not
// write.
func ParseArgs(s string) (id, action string, ok bool) {
	v, err := url.ParseQuery(s)
	if err != nil || v.Get("krate") != "1" {
		return "", "", false
	}
	return v.Get("id"), v.Get("action"), true
}

// URI is the activation URL of a click: scheme://activate?<Args>.
func URI(scheme, id, action string) string {
	return scheme + "://activate?" + Args(id, action)
}

// ParseURI reverses URI for the given scheme. Windows may add a trailing
// slash to the host part; both forms are accepted.
func ParseURI(scheme, raw string) (id, action string, ok bool) {
	rest, found := strings.CutPrefix(raw, scheme+"://")
	if !found {
		return "", "", false
	}
	rest = strings.TrimPrefix(rest, "activate")
	rest = strings.TrimPrefix(rest, "/")
	rest, found = strings.CutPrefix(rest, "?")
	if !found {
		return "", "", false
	}
	return ParseArgs(rest)
}

// FindArg returns the click carried by the first argument that is an
// activation URL of the scheme.
func FindArg(scheme string, args []string) (id, action string, ok bool) {
	for _, a := range args {
		if id, action, ok := ParseURI(scheme, a); ok {
			return id, action, true
		}
	}
	return "", "", false
}

// RegValue is one registry value to write (Name "" is the default value).
type RegValue struct {
	Key   string // below the root, backslash separated
	Name  string
	Value string
}

// RegistryValues returns the values that register the scheme under
// Software\Classes (HKCU for the running app, HKCR or HKLM\Software\Classes
// for an installer): the handler runs `"exe" "%1"`.
func RegistryValues(scheme, productName, exe string) []RegValue {
	base := `Software\Classes\` + scheme
	return []RegValue{
		{base, "", "URL:" + productName + " notification"},
		{base, "URL Protocol", ""},
		{base + `\DefaultIcon`, "", exe + ",0"},
		{base + `\shell\open\command`, "", `"` + exe + `" "%1"`},
	}
}
