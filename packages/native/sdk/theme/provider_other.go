//go:build !linux

package theme

func newDefaultProvider() provider { return nil }

func defaultAvailable() bool { return false }
