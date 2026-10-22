package renderer

import (
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// Owner tree emission.
//
// Pages that render a lowered React class component hydrate every component
// scope through the runtime's owner tree (packages/runtime/src/boundary.ts):
//
//	__krateOwnF("<id>",{p:"<parent>",n:"<Name>",c:"<cond>",b:"c",k:"<creator>"},function(){...});
//
// instead of a bare IIFE. The links recorded here let the runtime route errors
// to error boundaries, mount and unmount components with their conditional
// branch, order lifecycles children-first, and re-render the class components
// a re-rendering class created.

// claimOwnerLinks records node as the parent of every descendant signature
// that has no parent yet, and as the creator of those emitted from its own
// render (the first ownCount signatures; the rest are its call-site children,
// created by the caller).
func claimOwnerLinks(sigs []irtree.ComponentSignature, node *irtree.ComponentNode, ownCount int) {
	for i := range sigs {
		if sigs[i].ParentID == "" {
			sigs[i].ParentID = node.ID
		}
		if i < ownCount && sigs[i].CreatorID == "" {
			sigs[i].CreatorID = node.ID
		}
	}
}

// claimCond records the conditional branch a signature renders in, keeping
// the innermost one set by a nested conditional.
func claimCond(sigs []irtree.ComponentSignature, cond irtree.SlotID, branch string) {
	for i := range sigs {
		if sigs[i].CondID == "" {
			sigs[i].CondID = cond
			sigs[i].CondBranch = branch
		}
	}
}

// childrenPlaceholderCond finds the conditional branch holding a component's
// {props.children} placeholder, so the call-site children spliced into it
// mount and unmount with that branch (an error boundary's children branch).
func childrenPlaceholderCond(slots []irtree.SlotNode) (irtree.SlotID, string, bool) {
	for _, s := range slots {
		cs, ok := s.(*irtree.ConditionalSlot)
		if !ok {
			continue
		}
		if id, br, ok := childrenPlaceholderCond(cs.Consequent); ok {
			return id, br, true
		}
		if id, br, ok := childrenPlaceholderCond(cs.Alternate); ok {
			return id, br, true
		}
		if slotsHaveChildrenPlaceholder(cs.Consequent) {
			return cs.ID, "c", true
		}
		if slotsHaveChildrenPlaceholder(cs.Alternate) {
			return cs.ID, "a", true
		}
	}
	return "", "", false
}

func slotsHaveChildrenPlaceholder(slots []irtree.SlotNode) bool {
	for _, s := range slots {
		switch v := s.(type) {
		case *irtree.ChildrenSlot:
			return true
		case *irtree.StaticHTML:
			if strings.Contains(v.HTML, "<!--__children__-->") {
				return true
			}
		}
	}
	return false
}

// needsOwnerTree reports whether the page's hydration scopes are wrapped in
// owners: any lowered class component does (lifecycles, error boundaries).
func needsOwnerTree(sigs []irtree.ComponentSignature) bool {
	for _, sig := range sigs {
		if sig.Tier == irtree.TierClient && sig.Class {
			return true
		}
	}
	return false
}

// ownerPrelude defines the owner entry point once per page script; it falls
// back to running the scope directly when the runtime predates the owner tree.
const ownerPrelude = "var __krateOwnF=window.__krateOwn||function(i,m,f){f();};\n"

// ownerOpen opens a component scope registered with the owner tree.
func ownerOpen(sig irtree.ComponentSignature, present map[irtree.SlotID]bool) string {
	var b strings.Builder
	b.WriteString("__krateOwnF(")
	b.WriteString(strconv.Quote(string(sig.ComponentID)))
	b.WriteString(",{")
	first := true
	field := func(k, v string) {
		if v == "" {
			return
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(k)
		b.WriteByte(':')
		b.WriteString(strconv.Quote(v))
	}
	// Links to components without a hydration scope are dropped: the runtime
	// resolves ids against registered owners only.
	if present[sig.ParentID] {
		field("p", string(sig.ParentID))
	}
	field("n", sig.Name)
	if sig.CondID != "" {
		field("c", string(sig.CondID))
		field("b", sig.CondBranch)
	}
	if present[sig.CreatorID] {
		field("k", string(sig.CreatorID))
	}
	b.WriteString("},function(){\n")
	return b.String()
}
