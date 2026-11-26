//go:build js

package execx

import "os/exec"

// configureTree does nothing: js/wasm cannot start processes.
func configureTree(cmd *exec.Cmd) {}

// KillTree does nothing: js/wasm cannot start processes.
func KillTree(cmd *exec.Cmd) {}
