//go:build linux

package platform

import (
	"github.com/kratejs/krate/packages/native/platform/linux"
	"github.com/kratejs/krate/packages/native/platform/probe"
)

func probeOS() probe.Info { return linux.Probe() }
