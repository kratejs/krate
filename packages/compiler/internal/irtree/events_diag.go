package irtree

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// reactDOMEvents lists React DOM event props (without the on prefix and the
// Capture suffix), used to spot misspelled or lowercase handlers.
var reactDOMEvents = strings.Fields(`
	Abort AnimationEnd AnimationIteration AnimationStart AuxClick BeforeInput BeforeToggle Blur
	CanPlay CanPlayThrough Cancel Change Click Close CompositionEnd CompositionStart CompositionUpdate
	ContextMenu Copy Cut DoubleClick Drag DragEnd DragEnter DragExit DragLeave DragOver DragStart Drop
	DurationChange Emptied Encrypted Ended Error Focus GotPointerCapture Input Invalid KeyDown KeyPress
	KeyUp Load LoadStart LoadedData LoadedMetadata LostPointerCapture MouseDown MouseEnter MouseLeave
	MouseMove MouseOut MouseOver MouseUp Paste Pause Play Playing PointerCancel PointerDown PointerEnter
	PointerLeave PointerMove PointerOut PointerOver PointerUp Progress RateChange Reset Resize Scroll
	ScrollEnd Seeked Seeking Select Stalled Submit Suspend TimeUpdate Toggle TouchCancel TouchEnd
	TouchMove TouchStart TransitionCancel TransitionEnd TransitionRun TransitionStart VolumeChange
	Waiting Wheel
`)

// reactEventByLower maps a lowercased event name to its React spelling.
var reactEventByLower = func() map[string]string {
	m := make(map[string]string, len(reactDOMEvents))
	for _, n := range reactDOMEvents {
		m[strings.ToLower(n)] = n
	}
	return m
}()

// LowercaseEventReplacement returns the React camelCase spelling for a
// lowercase event prop such as onclick, onchange or onclickcapture. It returns
// false when the prop is not a lowercase React DOM event.
func LowercaseEventReplacement(prop string) (string, bool) {
	if len(prop) <= 2 || !strings.HasPrefix(prop, "on") {
		return "", false
	}
	name := strings.ToLower(prop[2:])
	capture := false
	if base, ok := strings.CutSuffix(name, "capture"); ok {
		if _, exists := reactEventByLower[base]; exists && base != "gotpointer" && base != "lostpointer" {
			name, capture = base, true
		}
	}
	want, ok := reactEventByLower[name]
	if !ok {
		return "", false
	}
	if capture {
		want += "Capture"
	}
	return "on" + want, true
}

// splitEventProp splits onClickCapture into ("Click", true). The pointer
// capture events keep their own Capture suffix.
func splitEventProp(prop string) (name string, capture bool) {
	name = prop[2:]
	if strings.HasSuffix(name, "Capture") && name != "GotPointerCapture" && name != "LostPointerCapture" {
		return strings.TrimSuffix(name, "Capture"), true
	}
	return name, false
}

// formReadOnlyValueTypes are input types whose value prop never needs an
// onChange (React's hasReadOnlyValue).
var formReadOnlyValueTypes = map[string]bool{
	"button": true, "checkbox": true, "image": true, "hidden": true,
	"radio": true, "reset": true, "submit": true,
}

// eventFormReporter receives a coded warning.
type eventFormReporter func(code diag.Code, pos ast.Pos, msg, hint string)

// checkEventsAndForms reports unsupported or suspicious event, form-control
// and ref patterns on a built-in element, mirroring React's development
// warnings at build time.
func checkEventsAndForms(el *ast.JSXElement, report eventFormReporter) {
	if el == nil || el.Opening == nil {
		return
	}
	tag := el.Opening.Name
	if tag == "" || tag[0] < 'a' || tag[0] > 'z' || strings.ContainsAny(tag, "-.:") {
		// Components and custom elements accept arbitrary props and events.
		return
	}
	attrs := map[string]*ast.JSXAttr{}
	hasSpread := false
	for _, a := range el.Opening.Attributes {
		if a == nil {
			continue
		}
		if a.Spread {
			hasSpread = true
			continue
		}
		attrs[a.Name] = a
	}
	at := func(a *ast.JSXAttr) ast.Pos {
		if a.Position.Line == 0 {
			return el.Position
		}
		return a.Position
	}

	for _, a := range el.Opening.Attributes {
		if a == nil || a.Spread || len(a.Name) <= 2 || !strings.HasPrefix(a.Name, "on") {
			continue
		}
		if isOnEvent(a.Name) {
			name, _ := splitEventProp(a.Name)
			if want, ok := reactEventByLower[strings.ToLower(name)]; ok && want == name {
				continue
			}
			hint := "check the spelling against React's event list; to listen for a custom DOM event, call ref.current.addEventListener in an effect"
			if want, ok := reactEventByLower[strings.ToLower(name)]; ok {
				hint = "did you mean on" + want + "?"
			} else if base, ok := strings.CutSuffix(strings.ToLower(name), "capture"); ok {
				if want, ok := reactEventByLower[base]; ok {
					hint = "did you mean on" + want + "Capture?"
				}
			}
			report(diag.CodeUnknownEvent, at(a), "unknown event handler `"+a.Name+"` on <"+tag+">: React ignores it", hint)
			continue
		}
		if a.Value == nil {
			continue
		}
		if lit, ok := a.Value.(*ast.Literal); ok && lit.Kind == ast.StringLit {
			// onclick="..." is an inline HTML handler string, not a React prop.
			continue
		}
		if want, ok := reactEventByLower[a.Name[2:]]; ok {
			report(diag.CodeLowercaseEvent, at(a), "invalid event handler `"+a.Name+"` on <"+tag+">: React event props are camelCase",
				"rename it to on"+want)
		}
	}

	// String refs (KR5003) are reported on the source by the bundler, which
	// also sees class components.

	if attrs["onChange"] != nil && attrs["contentEditable"] != nil && tag != "input" && tag != "textarea" && tag != "select" {
		report(diag.CodeContentEditableChange, at(attrs["onChange"]), "onChange never fires on a contentEditable <"+tag+">",
			"listen with onInput instead")
	}

	if tag != "input" && tag != "textarea" && tag != "select" {
		return
	}
	inputType := ""
	if t := attrs["type"]; t != nil {
		if lit, ok := t.Value.(*ast.Literal); ok && lit.Kind == ast.StringLit {
			inputType = strings.ToLower(lit.Value)
		}
	}
	handled := attrs["onChange"] != nil || attrs["onInput"] != nil || attrs["readOnly"] != nil || attrs["disabled"] != nil
	for _, pair := range [][2]string{{"value", "defaultValue"}, {"checked", "defaultChecked"}} {
		ctl, def := attrs[pair[0]], attrs[pair[1]]
		if ctl == nil || isUndefinedExpr(ctl.Value) {
			continue
		}
		if def != nil {
			report(diag.CodeValueAndDefault, at(def), "<"+tag+"> has both "+pair[0]+" and "+pair[1]+"; "+pair[1]+" is ignored",
				"keep "+pair[0]+" for a controlled field or "+pair[1]+" for an uncontrolled one, not both")
		}
		if switchesControl(ctl.Value) {
			empty := "''"
			if pair[0] == "checked" {
				empty = "false"
			}
			report(diag.CodeControlledSwitch, at(ctl), "<"+tag+"> "+pair[0]+" can switch between undefined (uncontrolled) and a value (controlled)",
				"use "+empty+" instead of undefined so the field stays controlled")
		}
		readOnlyType := pair[0] == "value" && tag == "input" && formReadOnlyValueTypes[inputType]
		if !handled && !hasSpread && !readOnlyType {
			report(diag.CodeReadOnlyControlled, at(ctl), "<"+tag+"> has "+pair[0]+" but no onChange handler, so it renders a read-only field",
				"add onChange to update the state, use "+pair[1]+" for an uncontrolled field, or set readOnly")
		}
	}
}

// shadowedFormDefault reports a defaultValue/defaultChecked that is ignored
// because the element also has value/checked (React uses the controlled prop,
// KR5005), so the HTML does not carry two value attributes.
func shadowedFormDefault(attrs []*ast.JSXAttr, name string) bool {
	var ctl string
	switch name {
	case "defaultValue":
		ctl = "value"
	case "defaultChecked":
		ctl = "checked"
	default:
		return false
	}
	for _, a := range attrs {
		if a != nil && !a.Spread && a.Name == ctl && a.Value != nil && !isUndefinedExpr(a.Value) {
			return true
		}
	}
	return false
}

// isUndefinedExpr reports a literal undefined or null value.
func isUndefinedExpr(e ast.Expr) bool {
	switch v := e.(type) {
	case nil:
		return false
	case *ast.Identifier:
		return v.Name == "undefined"
	case *ast.Literal:
		return v.Kind == ast.NullLit
	case *ast.UnaryExpr:
		return v.Op == "void"
	}
	return false
}

// switchesControl reports a conditional whose one branch is undefined/null
// and the other a value.
func switchesControl(e ast.Expr) bool {
	c, ok := e.(*ast.ConditionalExpr)
	if !ok {
		return false
	}
	return isUndefinedExpr(c.Consequent) != isUndefinedExpr(c.Alternate)
}

// warnEventsAndForms runs checkEventsAndForms once per source attribute
// position (a component instantiated many times reports once).
func (b *builder) warnEventsAndForms(el *ast.JSXElement) {
	if b.locateOnly != 0 {
		return
	}
	checkEventsAndForms(el, func(code diag.Code, pos ast.Pos, msg, hint string) {
		key := string(code) + "@" + b.srcFnName() + ":" + itoa(pos.Line) + ":" + itoa(pos.Col)
		if b.eventDiagSeen == nil {
			b.eventDiagSeen = map[string]bool{}
		}
		if b.eventDiagSeen[key] {
			return
		}
		b.eventDiagSeen[key] = true
		b.warnings = append(b.warnings, b.diagAt(pos, diag.SeverityWarning, msg, hint).WithCode(code))
	})
}
