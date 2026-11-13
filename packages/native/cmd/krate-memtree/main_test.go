package main

import (
	"reflect"
	"testing"
)

func TestParsePIDs(t *testing.T) {
	got, err := parsePIDs("12, 34,56")
	if err != nil || !reflect.DeepEqual(got, []int{12, 34, 56}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if got, err := parsePIDs(""); err != nil || len(got) != 0 {
		t.Fatalf("empty: %v, %v", got, err)
	}
	for _, bad := range []string{"x", "0", "-3"} {
		if _, err := parsePIDs(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}
