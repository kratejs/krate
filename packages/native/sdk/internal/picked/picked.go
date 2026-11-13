// Package picked connects the file dialogs to the fs scope without either
// importing the other (each can be left out of a build): the fs area adds a
// listener, the dialog area reports what the user chose.
package picked

import "sync"

var (
	mu        sync.RWMutex
	listeners []func(path string)
)

// OnPick adds a listener for paths the user chose in a file dialog.
func OnPick(f func(path string)) {
	mu.Lock()
	listeners = append(listeners, f)
	mu.Unlock()
}

// Notify reports one chosen path.
func Notify(path string) {
	mu.RLock()
	ls := append([]func(string){}, listeners...)
	mu.RUnlock()
	for _, f := range ls {
		f(path)
	}
}
