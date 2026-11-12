package darwin

import (
	"reflect"
	"testing"
)

func TestPathQueueHoldsUntilAHandlerIsSet(t *testing.T) {
	var q pathQueue
	q.push([]string{"/a.md"})
	q.push(nil)
	q.push([]string{"/b.md", "/c.md"})
	var got [][]string
	q.setHandler(func(p []string) { got = append(got, p) })
	q.push([]string{"/d.md"})
	want := [][]string{{"/a.md"}, {"/b.md", "/c.md"}, {"/d.md"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// What was held is delivered once.
	q.setHandler(func(p []string) { t.Errorf("redelivered %v", p) })
}

func TestPathQueueCopiesWhatItHolds(t *testing.T) {
	var q pathQueue
	p := []string{"/a.md"}
	q.push(p)
	p[0] = "/changed"
	var got []string
	q.setHandler(func(b []string) { got = append(got, b...) })
	if !reflect.DeepEqual(got, []string{"/a.md"}) {
		t.Fatalf("got %v", got)
	}
}

func TestPathQueueBoundsWhatItHolds(t *testing.T) {
	var q pathQueue
	for i := 0; i < maxHeldBatches+10; i++ {
		q.push([]string{"/x"})
	}
	n := 0
	q.setHandler(func([]string) { n++ })
	if n != maxHeldBatches {
		t.Fatalf("delivered %d batches, want %d", n, maxHeldBatches)
	}
}
