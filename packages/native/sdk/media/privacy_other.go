//go:build !linux

package media

import "github.com/kratejs/krate/packages/native/platform/caps"

// defaultPrivacy is the privacy source of a platform whose App does not
// implement caps.Privacy.
func defaultPrivacy() caps.Privacy { return nil }
