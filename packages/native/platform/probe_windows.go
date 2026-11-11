//go:build windows

package platform

import (
	"github.com/kratejs/krate/packages/native/platform/probe"
	"github.com/kratejs/krate/packages/native/platform/windows"
)

func probeOS() probe.Info { return windows.Probe() }
