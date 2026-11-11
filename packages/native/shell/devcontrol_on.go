//go:build krate_devcontrol

package shell

// allowDev lets a release executable take --dev: developer tools and the
// control channel on stdin (see README.md). Only builds made with the
// krate_devcontrol tag (the end-to-end tests) have it.
const allowDev = true
