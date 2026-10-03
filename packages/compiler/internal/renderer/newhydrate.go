package renderer

import (
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// HydrationBootstrapJS is the shared hydration infrastructure appended to the
// runtime chunk instead of being emitted into every page's JS. It defines the
// slot lookup helpers, the XSS sanitizer, and the compact binding helpers
// (kbind*) that per-page hydration scripts call.
//
// It runs once per page load. On SPA navigation the router swaps in the new
// page's DOM and re-runs that page's hydration script, which calls
// refreshSlots() to rebuild the comment-marker cache against the new DOM.
const HydrationBootstrapJS = `
window.__krateErrs=[];
window.__krate_props={};
window.$esc=function(v){var d=document.createElement('div');d.textContent=v;return d.innerHTML};
window.__safe=function(fn){try{return fn();}catch(e){try{window.__krateErrs.push(String((e&&e.stack)||(e&&e.message)||e));}catch(_){}return null;}};
(function(){
var __root=document.getElementById('root')||document.body;
var __cache=new Map();
function scan(){__cache.clear();var w=document.createTreeWalker(__root,NodeFilter.SHOW_COMMENT,null,false);while(w.nextNode()){var v=w.currentNode.nodeValue;if(v.indexOf('k:')===0)__cache.set(v.slice(2),w.currentNode);}}
scan();
window.findSlot=function(id){var el=__root.querySelector('[data-k="k:'+id+'"]');if(el)return el;return __cache.get(id)||null;};
window.refreshSlots=scan;
window.kbindText=function(id,get){var n=findSlot(id);if(!n)return;__safe(function(){createEffect(function(){var v=get();if(v==null||v===false)v='';if(n.nextSibling&&n.nextSibling.nodeType===3){n.nextSibling.textContent=String(v);}else{var t=document.createTextNode(String(v));n.parentNode.insertBefore(t,n.nextSibling);}});});};
window.kbindContent=function(id,get){var n=findSlot(id);if(!n)return;__safe(function(){createEffect(function(){var v=get();while(n.nextSibling&&!(n.nextSibling.nodeType===8&&n.nextSibling.nodeValue==='/k:'+n.nodeValue.slice(2))){var _d=n.nextSibling;n.parentNode.removeChild(_d);if(typeof disposeNode==='function')disposeNode(_d);}if(v==null||v===false)v='';if(typeof v==='string'||typeof v==='number')v=document.createTextNode(String(v));if(Array.isArray(v)){var ref=n.nextSibling;for(var i=0;i<v.length;i++)n.parentNode.insertBefore(v[i],ref);}else n.parentNode.insertBefore(v,n.nextSibling);});});};
window.kbindCond=function(id,get){var n=findSlot(id);if(!n)return;__safe(function(){createEffect(function(){var a=n.nextSibling,b=a?a.nextSibling:null;if(!a||!b)return;var v=(get());if(v){a.style.display='';b.style.display='none';}else{a.style.display='none';b.style.display='';}});});};
window.kbindAttr=function(id,attr,get){var n=findSlot(id);if(!n)return;__safe(function(){createEffect(function(){var v=get();if(v==null||v===false)n.removeAttribute(attr);else if(v===true)n.setAttribute(attr,'');else n.setAttribute(attr,String(v));});});};
window.kbindProp=function(id,attr,get){var n=findSlot(id);if(!n)return;__safe(function(){createEffect(function(){var v=get();if(attr==='value'){n.value=v==null||v===false?'':String(v);}else{n[attr]=!!v;}});});};
window.kbindHandler=function(id,prop,fn){var n=findSlot(id);if(n)__safe(function(){n[prop]=fn;});};
window.kbindEvent=function(id,ev,fn,capture){var n=findSlot(id);if(n)__safe(function(){n.addEventListener(ev,fn,!!capture);});};
window.kbindRef=function(id,set){var n=findSlot(id);if(n)__safe(function(){set(n);});};
})();
`

// GenerateNewHydrationJS produces per-component scoped hydration code.
// The shared infrastructure (findSlot, __safe, $esc, kbind* helpers) lives in
// the runtime chunk (HydrationBootstrapJS); this only emits the page-specific
// signal declarations and compact binding calls, so pages with no client
// signatures produce no JavaScript at all.
func GenerateNewHydrationJS(result *EmitResult) string {
	hasWork := false
	for _, sig := range result.Signatures {
		if sig.Tier != irtree.TierClient {
			continue
		}
		if len(sig.Signals) > 0 || len(sig.Handlers) > 0 || len(sig.Effects) > 0 ||
			len(sig.Memos) > 0 || len(sig.ExtraVars) > 0 || len(sig.PreSignalVars) > 0 ||
			len(sig.SlotBindings) > 0 || len(sig.AttrBindings) > 0 {
			hasWork = true
			break
		}
	}
	if !hasWork && !result.HasLinks {
		return ""
	}

	var b strings.Builder
	b.WriteString("(function(){\n")
	// Rebuild the comment-marker cache against the current DOM (fresh page
	// load or SPA navigation).
	b.WriteString("refreshSlots();\n")

	// Component functions needed by dynamic list slots. Without these the
	// runtime `h(Component, props)` call in a list binding would throw
	// ReferenceError when the list re-renders.
	for _, fn := range result.ListComponents {
		js := irtree.RenderComponentFnJS(fn)
		if js != "" {
			b.WriteString(js)
			b.WriteString("\n")
		}
	}

	// ─── Per-component scoped IIFEs ───────────────────────────────────
	for _, sig := range result.Signatures {
		if sig.Tier != irtree.TierClient {
			continue
		}
		b.WriteString("(function(){\n")

		// The component's own props object declaration must precede signal
		// initializers: signal RawInits like `createSignal(props.x || "")`
		// evaluate props at hydration time (they can't be const-folded when the
		// props are runtime values hoisted from the parent scope).
		for _, ev := range sig.ExtraVars {
			if strings.HasPrefix(ev, "var props=__krate_props[") {
				b.WriteString(ev)
				b.WriteString(";\n")
			}
		}

		// Local values a signal initializer may evaluate at hydration time
		// (e.g. createSignal(initial.value)) must be declared before the signal
		// declarations. These never read signals themselves, so ordering them
		// ahead is safe.
		for _, ev := range sig.PreSignalVars {
			b.WriteString(ev)
			b.WriteString(";\n")
		}

		for _, s := range sig.Signals {
			val := s.Initial
			if s.RawInit != "" {
				// Non-constant initializer (e.g. createSignal(Math.random())):
				// emit the real expression so the client evaluates it instead of
				// hydrating the signal to undefined.
				val = s.RawInit
			} else if s.IsString {
				val = "'" + escapeJSString(val) + "'"
			} else if !isJSNumberOrKeyword(val) {
				// Any resolved value that is not a recognised JS number/keyword
				// literal is string data whose type wasn't inferred (e.g.
				// "Hello world", "1.2.3", "NaN"). Quote it, or the emitted
				// createSignal(...) is invalid JS / references an undefined
				// global.
				val = "'" + escapeJSString(val) + "'"
			}
			factory := "createSignal(" + val + ")"
			if s.OptionsJS != "" {
				// Preserve the signal's options (e.g. `{ persist: 'key' }`) so
				// the runtime can restore/persist the value across reloads.
				factory = "createSignal(" + val + "," + s.OptionsJS + ")"
			}
			if s.FactoryJS != "" {
				// Reactive primitives whose setter is not a plain write (e.g.
				// createReducer) emit their full factory call instead.
				factory = s.FactoryJS
			}
			b.WriteString("const [")
			b.WriteString(s.Name)
			b.WriteString(",")
			b.WriteString(s.SetterName)
			b.WriteString("]=")
			b.WriteString(factory)
			b.WriteString(";\n")
		}

		// Extra variables (must come before effects/memos that may reference them)
		for _, ev := range sig.ExtraVars {
			if strings.HasPrefix(ev, "var props=__krate_props[") {
				continue
			}
			b.WriteString(ev)
			b.WriteString(";\n")
		}

		// Function-prop aliases (`var onNavigate = props.onNavigate`): live reads
		// of the registry object declared just above. These must precede the
		// effects/handlers that invoke the aliased function.
		for _, alias := range sig.FuncPropAliases {
			b.WriteString("var ")
			b.WriteString(alias)
			b.WriteString(";\n")
		}

		// Refs: assign the live DOM node to the referenced variable, or invoke a
		// callback ref with it. Runs after the extra vars are declared and before
		// effects/memos that read them, so an onMount/handler can safely use the
		// ref'd element.
		for _, rb := range sig.RefBindings {
			b.WriteString("kbindRef(")
			b.WriteString(strconv.Quote(string(rb.ElementSlotID)))
			if rb.Callback != "" {
				b.WriteString(",")
				b.WriteString(rb.Callback)
			} else if rb.Adaptive {
				// Handles every ref shape: a callback function, a {current} ref
				// object, or a plain variable that should be assigned the node.
				b.WriteString(",el=>{var _r=")
				b.WriteString(rb.Target)
				b.WriteString(";if(typeof _r==='function')_r(el);else if(_r&&typeof _r==='object')_r.current=el;else{")
				b.WriteString(rb.Target)
				b.WriteString("=el;}}")
			} else {
				b.WriteString(",el=>{")
				b.WriteString(rb.Target)
				b.WriteString("=el;}")
			}
			b.WriteString(")\n")
		}

		for i, memo := range sig.Memos {
			b.WriteString("const _memo")
			b.WriteString(itoa(i))
			b.WriteString("=")
			b.WriteString(memo)
			b.WriteString(";\n")
		}

		for _, eff := range sig.Effects {
			b.WriteString(eff)
			b.WriteString(";\n")
		}

		// Slot bindings: compact calls into the shared kbind* helpers.
		for _, sb := range sig.SlotBindings {
			if sb.ExprJS == "" {
				continue
			}
			switch sb.Type {
			case "text":
				b.WriteString("kbindText(")
				b.WriteString(strconv.Quote(string(sb.SlotID)))
				b.WriteString(",()=>")
				b.WriteString(sb.ExprJS)
				b.WriteString(");\n")
			case "expr", "list":
				b.WriteString("kbindContent(")
				b.WriteString(strconv.Quote(string(sb.SlotID)))
				b.WriteString(",()=>")
				b.WriteString(sb.ExprJS)
				b.WriteString(");\n")
			case "conditional":
				b.WriteString("kbindCond(")
				b.WriteString(strconv.Quote(string(sb.SlotID)))
				b.WriteString(",()=>(")
				b.WriteString(sb.ExprJS)
				b.WriteString("));\n")
			}
		}

		// Event handlers. Delegated (bubbling) handlers set a property that the
		// shared delegated listener invokes; capture-phase and non-bubbling
		// events get a direct listener on the element.
		for _, h := range sig.Handlers {
			if h.Direct {
				b.WriteString("kbindEvent(")
				b.WriteString(strconv.Quote(string(h.ElementSlotID)))
				b.WriteString(",")
				b.WriteString(strconv.Quote(h.Event))
				b.WriteString(",")
				b.WriteString(h.Body)
				b.WriteString(",")
				if h.Capture {
					b.WriteString("true")
				} else {
					b.WriteString("false")
				}
				b.WriteString(");\n")
				continue
			}
			propName := "__krate_" + h.Event + "_" + sanitizeHandlerProp(string(h.ElementSlotID))
			b.WriteString("kbindHandler(")
			b.WriteString(strconv.Quote(string(h.ElementSlotID)))
			b.WriteString(",")
			b.WriteString(strconv.Quote(propName))
			b.WriteString(",")
			b.WriteString(h.Body)
			b.WriteString(");\n")
		}

		// Attribute bindings: update the attribute on signal changes.
		for _, a := range sig.AttrBindings {
			exprJS := a.ExprSource
			if exprJS == "" && a.SignalName != "" {
				exprJS = a.SignalName + "()"
			}
			if exprJS == "" {
				continue
			}
			// value/checked/selected are DOM properties, not attributes: setting
			// the attribute after interaction has no effect.
			if isFormPropertyAttr(a.AttrName) {
				b.WriteString("kbindProp(")
			} else {
				b.WriteString("kbindAttr(")
			}
			b.WriteString(strconv.Quote(string(a.ElementSlotID)))
			b.WriteString(",")
			b.WriteString(strconv.Quote(a.AttrName))
			b.WriteString(",()=>")
			b.WriteString(exprJS)
			b.WriteString(");\n")
		}

		b.WriteString("})();\n")
	}

	// ─── Event delegation ─────────────────────────────────────────────
	seenEvents := map[string]bool{}
	var handlerEvents []string
	for _, sig := range result.Signatures {
		for _, h := range sig.Handlers {
			// Direct (capture / non-bubbling) handlers have their own listeners.
			if h.Direct {
				continue
			}
			if !seenEvents[h.Event] {
				seenEvents[h.Event] = true
				handlerEvents = append(handlerEvents, h.Event)
			}
		}
	}

	if len(handlerEvents) > 0 {
		b.WriteString("if(typeof __krate_del_cleanup==='function')__krate_del_cleanup();\n")
		// Same root resolution as the slot bootstrap (fall back to body), so a
		// non-#root container still works.
		b.WriteString("var __krate_del_root=document.getElementById('root')||document.body;\n")
		b.WriteString("var __krate_del_fns=[];\n")
		b.WriteString("function __krate_del_add(ev,fn){__krate_del_root.addEventListener(ev,fn);__krate_del_fns.push({ev:ev,fn:fn});}\n")
		for _, ev := range handlerEvents {
			// Event delegation: walk up from the target and invoke EVERY handler
			// on the path (bubbling), stopping only if the handler called
			// stopPropagation() (which sets e.cancelBubble).
			b.WriteString("__krate_del_add('")
			b.WriteString(ev)
			b.WriteString("',e=>{var _n=e.target;while(_n&&_n!==document){for(var _k in _n){if(_k.indexOf('__krate_")
			b.WriteString(ev)
			b.WriteString("_')===0){_n[_k](e);break;}}if(e.cancelBubble)return;_n=_n.parentNode;}});\n")
		}
		b.WriteString("__krate_del_cleanup=function(){for(var i=0;i<__krate_del_fns.length;i++){__krate_del_root.removeEventListener(__krate_del_fns[i].ev,__krate_del_fns[i].fn);}__krate_del_fns=[];};\n")
	}

	// ─── Router ───────────────────────────────────────────────────────
	b.WriteString("if(typeof reinitRouter==='function'){reinitRouter();}else if(typeof initRouter==='function'){initRouter();}\n")

	b.WriteString("})();\n")

	return b.String()
}

// sanitizeHandlerProp converts a slot ID to a safe JS property name component.
// Replaces dots and other non-alphanumeric chars with underscores.
func sanitizeHandlerProp(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			b.WriteByte(ch)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// isFormPropertyAttr reports whether an attribute is really a DOM property on
// form controls (value/checked/selected) that must be set via the property, not
// the attribute, to reflect programmatic updates after user interaction.
func isFormPropertyAttr(name string) bool {
	switch name {
	case "value", "checked", "selected":
		return true
	}
	return false
}

// isJSNumberOrKeyword reports whether v is a valid JS numeric literal or a
// boolean/null keyword, and therefore safe to emit unquoted.
func isJSNumberOrKeyword(v string) bool {
	switch v {
	case "true", "false", "null", "undefined", "NaN", "Infinity", "-Infinity":
		return true
	}
	if v == "" {
		return false
	}
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return true
	}
	return false
}

// isBareStringToken reports whether v is a bare JS identifier that is not a
// literal keyword (true/false/null/undefined/NaN/Infinity). Signal initial
// values that come from string-default expressions (props.x || "") are
// resolved to their string value at build time but may not be type-inferred
// as strings; quoting them avoids emitting an undefined-global reference.
func isBareStringToken(v string) bool {
	switch v {
	case "true", "false", "null", "undefined", "NaN", "Infinity":
		return false
	}
	if v == "" {
		return false
	}
	for i := 0; i < len(v); i++ {
		ch := v[i]
		if i == 0 {
			if !(ch == '_' || ch == '$' || (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z')) {
				return false
			}
		} else if !(ch == '_' || ch == '$' || (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')) {
			return false
		}
	}
	return true
}
