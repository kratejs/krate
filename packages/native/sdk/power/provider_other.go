//go:build !linux

package power

func newDefaultProvider() provider { return nil }

func defaultAvailable() bool { return false }
