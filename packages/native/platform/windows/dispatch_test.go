package windows

import "testing"

// A push made while a drained function is still running (a modal dialog on
// the main thread) must ask for its own wake, or it waits for the dialog.
func TestQueuePushDuringDrainWakes(t *testing.T) {
	var q queue
	var wakeDuring bool
	ran := 0
	if !q.push(func() {
		wakeDuring = q.push(func() { ran++ })
	}) {
		t.Fatal("the first push must ask for a wake")
	}
	q.drain()
	if !wakeDuring {
		t.Error("a push during a running function did not ask for a wake")
	}
	if ran != 1 {
		t.Errorf("the function pushed during the drain ran %d times, want 1", ran)
	}
	if !q.push(func() {}) {
		t.Error("after a full drain the next push must ask for a wake")
	}
	if q.push(func() {}) {
		t.Error("a second push before the drain must not ask for another wake")
	}
}
