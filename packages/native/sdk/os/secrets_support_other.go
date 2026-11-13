//go:build !linux

package sdkos

func secretsSupported() bool { return backend != nil }
