package dbusmini

import (
	"math"
	"reflect"
	"sort"
)

func mathFloat64bits(f float64) uint64     { return math.Float64bits(f) }
func mathFloat64frombits(b uint64) float64 { return math.Float64frombits(b) }

// sortKeys orders map keys so that encoding is deterministic.
func sortKeys(keys []reflect.Value) {
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		switch a.Kind() {
		case reflect.String:
			return a.String() < b.String()
		case reflect.Int, reflect.Int16, reflect.Int32, reflect.Int64:
			return a.Int() < b.Int()
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return a.Uint() < b.Uint()
		}
		return false
	})
}
