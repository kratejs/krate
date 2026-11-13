//go:build linux

package sdkos

func secretsSupported() bool { return secretsAvailable() }
