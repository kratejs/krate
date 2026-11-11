//go:build darwin

package platform

import (
	"github.com/kratejs/krate/packages/native/platform/darwin"
	"github.com/kratejs/krate/packages/native/platform/probe"
)

func probeOS() probe.Info { return darwin.Probe() }
