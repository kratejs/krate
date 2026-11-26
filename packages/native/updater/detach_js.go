//go:build js

package updater

import "os/exec"

// detach does nothing: js/wasm cannot start processes.
func detach(cmd *exec.Cmd) {}
