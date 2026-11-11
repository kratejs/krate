//go:build !linux && !darwin && !windows

package platform

import "github.com/kratejs/krate/packages/native/platform/probe"

func probeOS() probe.Info { return unsupportedProbe() }
