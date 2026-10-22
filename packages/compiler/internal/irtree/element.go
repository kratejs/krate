package irtree

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/htmlspec"
)

// buildStaticElementSlots builds an intrinsic HTML element. When the element
// has component or dynamic children, the result is split:
//
//	[StaticHTML(opening+pre), ComponentSlot, StaticHTML(post+closing)]
//
// When all children are static, it returns a single StaticHTML.
func (b *builder) buildStaticElementSlots(el *ast.JSXElement, parentID string) []SlotNode {
	logicalID := joinSlotID(parentID, b.nextElementTag(el.Opening.Name, parentID))
	id := b.assignSlotID(logicalID)
	b.addMarkupErrs(CheckElementMarkup(el, func(pos ast.Pos, msg, hint string) error {
		return b.diagAt(pos, diag.SeverityError, msg, hint)
	}))
	tag := el.Opening.Name

	// dangerouslySetInnerHTML={{__html: "..."}} injects raw, pre-rendered HTML
	// (e.g. build-time markdown). When statically resolvable, the whole element
	// becomes a single StaticHTML slot so the value is never HTML-escaped.
	if rawHTML, ok := b.evalInnerHTMLAttr(el); ok {
		openingTag := b.buildElementOpening(el, nil, nil, nil, string(id))
		return []SlotNode{&StaticHTML{HTML: openingTag + ">" + rawHTML + "</" + el.Opening.Name + ">"}}
	}

	handlers, attrs, refs := b.elementBindings(el, id)
	// Accumulate the bindings for the component node.
	b.pendingHandlers = append(b.pendingHandlers, handlers...)
	b.pendingAttrs = append(b.pendingAttrs, attrs...)
	b.pendingRefs = append(b.pendingRefs, refs...)

	// A <select value>/<select defaultValue> marks its matching options
	// selected; the selection is visible to options built below it.
	if tag == "select" {
		sel, _ := SelectSelection(el, b.evalAttrConst)
		b.selectStack = append(b.selectStack, sel)
		defer func() { b.selectStack = b.selectStack[:len(b.selectStack)-1] }()
	}

	children := b.elementChildren(el, id)
	openingTag := b.buildElementOpening(el, handlers, attrs, refs, string(id))
	return assembleElement(el, openingTag, children)
}

// elementBindings collects an element's event handlers, dynamic attribute
// bindings and refs. A spread whose value is a known rest parameter or a
// build-time object is expanded first, so forwarded props reach the element;
// any other spread is reported, since its attributes cannot be rendered.
func (b *builder) elementBindings(el *ast.JSXElement, id SlotID) (handlers []HandlerDecl, attrs []AttrBinding, refs []RefBinding) {
	attributes, _ := b.expandForwardedSpreadAttrs(el.Opening.Attributes)
	for _, attr := range attributes {
		if attr.Spread {
			if attr.Value != nil && !isScopeHelperExpr(attr.Value) && b.locateOnly == 0 {
				b.warnAt(attr.Position, "spread attribute {..."+generateExprJS(attr.Value, nil)+"} on <"+el.Opening.Name+"> has no build-time value; its attributes are not rendered",
					"spread a rest/props parameter or an object whose value is known at build time, or pass the attributes explicitly")
			}
			continue
		}
		// React-only directives (key, suppressHydrationWarning) carry no DOM
		// meaning and must not become attributes or hydration bindings.
		if isReactDirectiveAttr(attr.Name) {
			continue
		}
		switch {
		case attr.Name == "ref":
			if rb, ok := b.refBinding(attr, id); ok {
				refs = append(refs, rb)
			}
		case isOnEvent(attr.Name):
			if h := b.buildHandlerDecl(attr, string(id)); h != nil {
				handlers = append(handlers, *h)
			}
		case isFormActionAttr(attr.Name) && attr.Value != nil && b.isFunctionAction(attr.Value):
			// React 19 function actions run through the runtime submit
			// listener; the value is bound to the element, not rendered.
			if rb, ok := b.formActionBinding(attr, id); ok {
				refs = append(refs, rb)
			}
		case isAttrBinding(attr):
			if a := b.buildAttrBinding(attr, string(id)); a != nil {
				attrs = append(attrs, *a)
			}
		}
	}
	return handlers, attrs, refs
}

// refBinding lowers a `ref` attribute to its hydration binding.
func (b *builder) refBinding(attr *ast.JSXAttr, id SlotID) (RefBinding, bool) {
	if attr.Value == nil {
		return RefBinding{}, false
	}
	// Callback ref: ref={(el) => {...}}. The arrow function is the callback
	// itself - it receives the mounted element directly (React-style).
	if fn, ok := attr.Value.(*ast.ArrowFn); ok {
		cb := renderArrowFn(fn, b.sigMap())
		return RefBinding{ElementSlotID: id, Callback: cb}, cb != ""
	}
	target := generateExprJS(attr.Value, b.sigMap())
	if target == "" {
		return RefBinding{}, false
	}
	// ref={fnName} where fnName is a function declared in this component is
	// a callback ref: kbindRef INVOKES it with the mounted element. An
	// assignment target would emit el=>{fnName=el;} and overwrite the
	// function variable.
	if refID, ok := attr.Value.(*ast.Identifier); ok && b.refCallbackVars != nil && b.refCallbackVars[refID.Name] {
		return RefBinding{ElementSlotID: id, Callback: refID.Name}, true
	}
	// An identifier or member ref (`ref={inputRef}`, a forwarded
	// `ref={props.ref}`) may be a `{current}` object, a callback, or a plain
	// variable: the adaptive setter handles all three.
	switch attr.Value.(type) {
	case *ast.Identifier, *ast.MemberExpr:
		return RefBinding{ElementSlotID: id, Target: target, Adaptive: true}, true
	}
	return RefBinding{ElementSlotID: id, Target: target}, true
}

// elementChildren builds an element's children. JSX text is decoded by the
// parser, so it is escaped here; raw-text elements (script/style) keep their
// content verbatim with only the closing-tag sequence neutralized. A
// <textarea value/defaultValue> renders the value as its content.
func (b *builder) elementChildren(el *ast.JSXElement, id SlotID) []SlotNode {
	tag := el.Opening.Name
	rawText := htmlspec.IsRawText(tag)
	var children []SlotNode
	for _, child := range el.Children {
		switch c := child.(type) {
		case *ast.JSXText:
			if c.Value == "" {
				continue
			}
			if rawText {
				children = append(children, &StaticHTML{HTML: htmlspec.RawText(tag, c.Value)})
			} else {
				children = append(children, &StaticHTML{HTML: escape.Text(c.Value)})
			}
		case *ast.JSXExprContainer:
			if !rawText {
				children = append(children, b.buildExprContainerChildren(c, string(id))...)
				continue
			}
			for _, n := range b.buildMetaExprContainerChildren(c, string(id)) {
				if st, ok := n.(*StaticHTML); ok {
					children = append(children, &StaticHTML{HTML: htmlspec.RawText(tag, st.HTML)})
				} else {
					children = append(children, n)
				}
			}
		case *ast.JSXElementChild:
			children = append(children, b.buildSlotNodes(c.Element, string(id))...)
		case *ast.JSXFragmentChild:
			children = append(children, b.buildFragmentSlots(c.Fragment, string(id))...)
		}
	}
	if tag == "textarea" {
		if content, ok := TextareaValue(el, b.evalAttrConst); ok {
			children = []SlotNode{&StaticHTML{HTML: content}}
		}
	}
	if len(children) > 0 {
		if st, ok := children[0].(*StaticHTML); ok {
			if fixed := PreserveLeadingNewline(tag, st.HTML); fixed != st.HTML {
				children[0] = &StaticHTML{HTML: fixed}
			}
		}
	}
	return children
}

// assembleElement joins an element's opening tag, children and closing tag,
// splitting into StaticHTML segments around dynamic children. Void elements
// never get children or a closing tag (children on a void element are
// reported by CheckElementMarkup); other self-closing elements need an
// explicit closing tag.
func assembleElement(el *ast.JSXElement, openingTag string, children []SlotNode) []SlotNode {
	tag := el.Opening.Name
	closingTag := "</" + tag + ">"
	if htmlspec.IsVoid(tag) {
		return []SlotNode{&StaticHTML{HTML: openingTag + " />"}}
	}
	if el.Opening.SelfClosing && len(children) == 0 {
		return []SlotNode{&StaticHTML{HTML: openingTag + "></" + tag + ">"}}
	}
	var result []SlotNode
	var buf strings.Builder
	buf.WriteString(openingTag)
	buf.WriteByte('>')
	for _, child := range children {
		switch c := child.(type) {
		case nil:
		case *StaticHTML:
			buf.WriteString(c.HTML)
		case *ChildrenSlot:
			buf.WriteString("<!--__children__-->")
		default:
			// Flush the static prefix, then the dynamic child.
			result = append(result, &StaticHTML{HTML: buf.String()})
			buf.Reset()
			result = append(result, child)
		}
	}
	buf.WriteString(closingTag)
	return append(result, &StaticHTML{HTML: buf.String()})
}
