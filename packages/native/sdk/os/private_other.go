//go:build !unix

package sdkos

// checkPrivate has nothing to check where directories inherit per-user ACLs.
func checkPrivate(string) error { return nil }
