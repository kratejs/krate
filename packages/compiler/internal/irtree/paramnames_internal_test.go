package irtree

import (
	"reflect"
	"testing"
)

// A nested pattern's default (`tags: [first, ...rest] = []`) is not part of
// the names it binds.
func TestDestructuredParamNamesNested(t *testing.T) {
	cases := map[string][]string{
		"{ item: { id, tags: [first = 'none', ...rest] = [] }, idx }": {"id", "first", "rest", "idx"},
		"{ pair: [a, [b, ...c] = [], ...d], n }":                      {"a", "b", "c", "d", "n"},
		"{ opts: { x = 1, ...o } = {} }":                              {"x", "o"},
		"{ a: [x] = [1], b: { y } = {} }":                             {"x", "y"},
		"{ a, b = 1, c: renamed }":                                    {"a", "b", "renamed"},
	}
	for pattern, want := range cases {
		if got := destructuredParamNames(pattern); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v, want %v", pattern, got, want)
		}
	}
}
