package irtree

import "sort"

// referencedModuleConstVars returns `var NAME=<init>` declarations for
// module-level constants used by client-only code (handlers, effects, memos,
// attribute bindings, signal factories). Those expressions are emitted into a
// per-component hydration IIFE where module bindings are not in scope.
func (b *builder) referencedModuleConstVars(node *ComponentNode) []string {
	if node == nil || len(b.moduleConstInits) == 0 {
		return nil
	}

	var sources []string
	for _, h := range node.Handlers {
		sources = append(sources, h.Body)
	}
	for _, eff := range node.Effects {
		sources = append(sources, eff)
	}
	for _, memo := range node.Memos {
		sources = append(sources, memo)
	}
	for _, attr := range node.AttrBindings {
		sources = append(sources, attr.ExprSource)
	}
	for _, sig := range node.Signals {
		sources = append(sources, sig.FactoryJS, sig.RawInit)
	}
	sources = append(sources, node.ExtraVars...)
	sources = append(sources, node.PreSignalVars...)

	used := make(map[string]bool)
	var queue []string
	for name := range b.moduleConstInits {
		for _, src := range sources {
			if src != "" && codeContainsIdent(src, name) {
				used[name] = true
				queue = append(queue, name)
				break
			}
		}
	}

	// Follow constants referenced by other constants' initializers.
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		init := b.moduleConstInits[name]
		if init == nil {
			continue
		}
		js := generateExprJS(init, nil)
		if js == "" {
			continue
		}
		for other := range b.moduleConstInits {
			if !used[other] && codeContainsIdent(js, other) {
				used[other] = true
				queue = append(queue, other)
			}
		}
	}

	if len(used) == 0 {
		return nil
	}
	names := make([]string, 0, len(used))
	for name := range used {
		names = append(names, name)
	}
	sort.Strings(names)

	decls := make([]string, 0, len(names))
	for _, name := range names {
		js := generateExprJS(b.moduleConstInits[name], nil)
		if js == "" {
			continue
		}
		decls = append(decls, "var "+name+"="+js)
	}
	return decls
}
