package darwin

import (
	"sync"
	"testing"
)

func TestWorkQueueKeysAreUniqueAndNonzero(t *testing.T) {
	q := newWorkQueue()
	var wg sync.WaitGroup
	keys := make(chan uintptr, 1000)
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			keys <- q.add(func() {})
		}()
	}
	wg.Wait()
	close(keys)
	seen := map[uintptr]bool{}
	for k := range keys {
		if k == 0 || seen[k] {
			t.Fatalf("bad key %d", k)
		}
		seen[k] = true
	}
	if q.len() != 1000 {
		t.Fatalf("len = %d", q.len())
	}
}

func TestWorkQueueTakeOnce(t *testing.T) {
	q := newWorkQueue()
	ran := 0
	k := q.add(func() { ran++ })
	if fn := q.take(k); fn == nil {
		t.Fatal("missing")
	} else {
		fn()
	}
	if q.take(k) != nil || q.take(12345) != nil {
		t.Fatal("unexpected entry")
	}
	if ran != 1 || q.len() != 0 {
		t.Fatalf("ran %d len %d", ran, q.len())
	}
}

func TestWorkQueueSkipsBusyKeyOnWrap(t *testing.T) {
	q := newWorkQueue()
	first := q.add(func() {})
	q.seq = first - 1
	second := q.add(func() {})
	if second == first {
		t.Fatal("reused a live key")
	}
}
