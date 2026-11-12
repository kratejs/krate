package crash

import (
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLimiter(3, 30*time.Second)
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if !l.allow(1) {
			t.Fatalf("reload %d refused", i)
		}
		now = now.Add(time.Second)
	}
	if l.allow(1) {
		t.Error("a fourth reload inside the window must be refused")
	}
	if !l.allow(2) {
		t.Error("views are counted separately")
	}
	now = now.Add(31 * time.Second)
	if !l.allow(1) {
		t.Error("the window slides")
	}
}
