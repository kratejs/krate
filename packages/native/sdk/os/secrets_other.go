//go:build !linux && !darwin && !windows

package sdkos

func platformBackend() secretBackend { return nil }
