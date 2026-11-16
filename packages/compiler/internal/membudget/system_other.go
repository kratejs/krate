//go:build !linux && !darwin && !windows

package membudget

func readSystem() system { return system{} }
