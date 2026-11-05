package annotator

import (
	"regexp"
	"sort"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// Components of the server, static and runtime tiers render to HTML and ship
// no JavaScript. State, effects and event handlers in one would build and
// leave a dead page, so they are reported instead.

// interactiveHooks are the hooks (as lowered by the React rewrite, plus the
// ones kept by name) and Krate primitives that need a client runtime.
// useMemo, useCallback, useId, useRef, useContext and use() also run during a
// server render and are allowed.
var interactiveHooks = map[string]bool{
	"createSignal": true, "createEffect": true, "createEffectDeps": true, "createEffectEvent": true, "createReducer": true,
	"createResource": true, "createStore": true, "onMount": true, "onCleanup": true,
	"useState": true, "useReducer": true, "useEffect": true, "useLayoutEffect": true,
	"useInsertionEffect": true, "useTransition": true, "useOptimistic": true,
	"useActionState": true, "useFormStatus": true, "useSyncExternalStore": true,
	"useImperativeHandle": true, "useEffectEvent": true,
}

// loweredHooks maps a lowered primitive back to the React hooks that produce
// it, so a diagnostic can name what the author wrote.
var loweredHooks = map[string][]string{
	"createSignal":     {"useState"},
	"createEffect":     {"useEffect", "useLayoutEffect", "useInsertionEffect"},
	"createEffectDeps": {"useEffect", "useLayoutEffect", "useInsertionEffect"},
	"createReducer":    {"useReducer"},
}

var (
	handlerAttrRe  = regexp.MustCompile(`^on[A-Z]`)
	reactHookCall  = regexp.MustCompile(`\b(use[A-Z]\w*)\s*\(`)
	classLifecycle = map[string]bool{
		"componentDidMount": true, "componentDidUpdate": true, "componentWillUnmount": true,
		"getSnapshotBeforeUpdate": true, "componentDidCatch": true,
	}
)

// CheckInteractiveTierComponents reports client-only APIs and event handlers in
// components of a build-time tier (@server, @static, @runtime). actions maps
// the names that are server actions: an action may be bound to a handler.
// Errors come back in a stable order.
func CheckInteractiveTierComponents(ann *irtree.Annotations, actions map[string]string) []error {
	if ann == nil {
		return nil
	}
	names := make([]string, 0, len(ann.UsedComponents))
	for name := range ann.UsedComponents {
		names = append(names, name)
	}
	sort.Strings(names)
	var errs []error
	seen := map[*ast.FnDecl]bool{}
	for _, name := range names {
		fn := ann.Functions[name]
		if fn == nil || seen[fn] || fn.Async {
			continue
		}
		tier := ann.ComponentTiers[name]
		if tier != irtree.TierServer && tier != irtree.TierStatic && tier != irtree.TierRuntime {
			continue
		}
		seen[fn] = true
		errs = append(errs, interactiveErrors(ann, name, fn, tier.String(), actions)...)
	}
	return errs
}

func interactiveErrors(ann *irtree.Annotations, name string, fn *ast.FnDecl, tier string, actions map[string]string) []error {
	file := ann.ComponentSources[name]
	if file == "" {
		file = ann.SourceFile
	}
	raw := ann.ComponentRaw[name]
	display := bundler.OriginalName(name)
	if fn.Name != "" {
		display = bundler.OriginalName(fn.Name)
	}
	where := display + ", a " + tier + " component"
	fix := "make " + display + " a client component by removing its " + tier + " tier (the @" + tier + " comment or decorator, the ." + tier + " file name, or the config entry), or move the interactive part into a client child component (one with no tier, or marked @client) and render that from " + display

	var errs []error
	add := func(pos ast.Pos, msg string) {
		errs = append(errs, diag.New(file, pos.Line, pos.Col, msg, fix, lineOf(raw, pos.Line)).WithCode(diag.CodeClientAPIInServer))
	}
	isClass := ann.ClassComponents[name]
	irtree.WalkAST(fn.Body, func(n any) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			callee := calleeName(x.Callee)
			if isClass {
				switch {
				case callee == "setState" || callee == "forceUpdate":
					add(x.Position, "this."+callee+"() in "+where+", which renders once at build time and ships no JavaScript; its state can never change")
				case callee == "__krateClass" && len(x.Args) >= 4:
					if obj, ok := x.Args[3].(*ast.ObjectExpr); ok {
						for _, p := range obj.Properties {
							if p != nil && classLifecycle[p.Key] {
								add(x.Position, p.Key+"() in "+where+", which ships no JavaScript; lifecycle methods never run")
							}
						}
					}
				}
				return true
			}
			if interactiveHooks[callee] {
				add(x.Position, hookName(callee, lineOf(raw, x.Position.Line))+"() in "+where+", which renders once at build time and ships no JavaScript; state and effects cannot work there")
			}
		case *ast.JSXElement:
			if x.Opening == nil {
				return true
			}
			tag := x.Opening.Name
			if tag == "" || tag[0] < 'a' || tag[0] > 'z' {
				return true
			}
			for _, a := range x.Opening.Attributes {
				if a == nil || a.Spread || !handlerAttrRe.MatchString(a.Name) || a.Value == nil {
					continue
				}
				if id, ok := a.Value.(*ast.Identifier); ok && actions[id.Name] != "" {
					continue
				}
				add(a.Position, "event handler "+a.Name+" on <"+tag+"> in "+where+", which ships no JavaScript; server-rendered HTML has no event handlers")
			}
		}
		return true
	})
	return errs
}

// hookName names the hook the author wrote for a lowered call, such as
// useState for createSignal, when the source line shows it.
func hookName(callee, line string) string {
	for _, m := range reactHookCall.FindAllStringSubmatch(line, -1) {
		for _, o := range loweredHooks[callee] {
			if m[1] == o {
				return o
			}
		}
	}
	return callee
}

func calleeName(e ast.Expr) string {
	switch c := e.(type) {
	case *ast.Identifier:
		return c.Name
	case *ast.MemberExpr:
		if p, ok := c.Property.(*ast.Identifier); ok && !c.Computed {
			return p.Name
		}
	}
	return ""
}
