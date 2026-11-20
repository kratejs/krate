//go:build !unix

package build

// The server fixtures behind e2eEnv need a unix host (the Node sidecars they
// start are stopped by process group), so they are skipped on this platform.
const canStopInProcessServer = false
