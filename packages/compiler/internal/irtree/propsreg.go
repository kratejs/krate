package irtree

import (
	"strconv"
	"strings"
)

// PropsReg is a child component's `__krate_props["<id>"]={...}` registration,
// evaluated in its parent's hydration scope.
type PropsReg struct {
	// ID is the registry key, the child instance's slot id.
	ID      string
	Entries []PropsRegEntry
}

// PropsRegEntry is one call-site prop of a registration. Value is JS in the
// parent's scope; a Getter entry re-evaluates it on every read.
type PropsRegEntry struct {
	Name   string
	Value  string
	Getter bool
}

// JS renders the registration. keep, when non-nil, selects the entries to
// emit; the others are left out of the object.
func (r PropsReg) JS(keep func(name string) bool) string {
	var sb strings.Builder
	sb.WriteString("__krate_props[")
	sb.WriteString(strconv.Quote(r.ID))
	sb.WriteString("]={")
	first := true
	for _, e := range r.Entries {
		if keep != nil && !keep(e.Name) {
			continue
		}
		if !first {
			sb.WriteByte(',')
		}
		first = false
		if e.Getter {
			sb.WriteString("get ")
			sb.WriteString(jsObjectKey(e.Name))
			sb.WriteString("(){return ")
			sb.WriteString(e.Value)
			sb.WriteString("}")
			continue
		}
		sb.WriteString(jsObjectKey(e.Name))
		sb.WriteByte(':')
		sb.WriteString(e.Value)
	}
	sb.WriteByte('}')
	return sb.String()
}
