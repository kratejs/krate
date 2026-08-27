package renderer

import (
	"strconv"
	"strings"

	"krate-compiler/internal/irtree"
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
window.__safe=function(fn){try{return fn();}catch(e){try{window.__krateErrs.push(String(e&&e.message));}catch(_){}return null;}};
(function(){
var __root=document.getElementById('root')||document.body;
var __cache=new Map();
function scan(){__cache.clear();var w=document.createTreeWalker(__root,NodeFilter.SHOW_COMMENT,null,false);while(w.nextNode()){var v=w.currentNode.nodeValue;if(v.indexOf('k:')===0)__cache.set(v.slice(2),w.currentNode);}}
scan();
window.findSlot=function(id){var el=__root.querySelector('[data-k="k:'+id+'"]');if(el)return el;return __cache.get(id)||null;};
window.refreshSlots=scan;
window.kbindText=function(id,get){var n=findSlot(id);if(!n)return;__safe(function(){createEffect(function(){var v=get();if(n.nextSibling&&n.nextSibling.nodeType===3){n.nextSibling.textContent=String(v);}else{var t=document.createTextNode(String(v));n.parentNode.insertBefore(t,n.nextSibling);}});});};
window.kbindContent=function(id,get){var n=findSlot(id);if(!n)return;__safe(function(){createEffect(function(){var v=get();while(n.nextSibling&&!(n.nextSibling.nodeType===8&&n.nextSibling.nodeValue==='/k:'+n.nodeValue.slice(2))){n.parentNode.removeChild(n.nextSibling);}if(v==null||v===false)v='';if(typeof v==='string'||typeof v==='number')v=document.createTextNode(String(v));if(Array.isArray(v)){for(var i=0;i<v.length;i++)n.parentNode.insertBefore(v[i],n.nextSibling);}else n.parentNode.insertBefore(v,n.nextSibling);});});};
window.kbindCond=function(id,get){var n=findSlot(id);if(!n)return;var a=n.nextSibling,b=a?a.nextSibling:null;if(!a||!b)return;__safe(function(){createEffect(function(){var v=(get());if(v){a.style.display='';b.style.display='none';}else{a.style.display='none';b.style.display='';}});});};
window.kbindAttr=function(id,attr,get){var n=findSlot(id);if(!n)return;__safe(function(){createEffect(function(){var v=get();if(v==null||v===false)n.removeAttribute(attr);else if(v===true)n.setAttribute(attr,'');else n.setAttribute(attr,String(v));});});};
window.kbindHandler=function(id,prop,fn){var n=findSlot(id);if(n)__safe(function(){n[prop]=fn;});};
window.kbindRef=function(id,set){var n=findSlot(id);if(n)set(n);};
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
			len(sig.Memos) > 0 || len(sig.ExtraVars) > 0 || len(sig.SlotBindings) > 0 ||
			len(sig.AttrBindings) > 0 {
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

		for _, s := range sig.Signals {
			val := s.Initial
			if s.IsString {
				val = "'" + escapeJSString(val) + "'"
			} else if isBareStringToken(val) {
				// Resolved string initial values that weren't type-inferred as
				// strings (e.g. props.x || "") must still be emitted as quoted
				// literals or they reference an undefined global at runtime.
				val = "'" + escapeJSString(val) + "'"
			}
			b.WriteString("const [")
			b.WriteString(s.Name)
			b.WriteString(",")
			b.WriteString(s.SetterName)
			b.WriteString("]=createSignal(")
			b.WriteString(val)
			b.WriteString(");\n")
		}

		// Extra variables (must come before effects/memos that may reference them)
		for _, ev := range sig.ExtraVars {
			b.WriteString(ev)
			b.WriteString(";\n")
		}

		// Refs: assign the live DOM node to the referenced variable. Runs after
		// the extra vars are declared and before effects/memos that read them,
		// so an onMount/handler can safely use the ref'd element.
		for _, rb := range sig.RefBindings {
			b.WriteString("kbindRef(")
			b.WriteString(strconv.Quote(string(rb.ElementSlotID)))
			b.WriteString(",el=>{")
			b.WriteString(rb.Target)
			b.WriteString("=el;})\n")
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

		// Event handlers: find element by slot ID, set __krate_{event}_{slotID}
		for _, h := range sig.Handlers {
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
			b.WriteString("kbindAttr(")
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
			if !seenEvents[h.Event] {
				seenEvents[h.Event] = true
				handlerEvents = append(handlerEvents, h.Event)
			}
		}
	}

	if len(handlerEvents) > 0 {
		b.WriteString("if(typeof __krate_del_cleanup==='function')__krate_del_cleanup();\n")
		b.WriteString("var __krate_del_root=document.querySelector('#root');\n")
		b.WriteString("var __krate_del_fns=[];\n")
		b.WriteString("function __krate_del_add(ev,fn){__krate_del_root.addEventListener(ev,fn);__krate_del_fns.push({ev:ev,fn:fn});}\n")
		for _, ev := range handlerEvents {
			// Event delegation: walk up from target, check for __krate_{event}_{any} property
			b.WriteString("__krate_del_add('")
			b.WriteString(ev)
			b.WriteString("',e=>{var _n=e.target;while(_n&&_n!==document){var _found=false;for(var _k in _n){if(_k.indexOf('__krate_")
			b.WriteString(ev)
			b.WriteString("_')===0){_n[_k](e);_found=true;break;}}if(_found)break;_n=_n.parentNode;}});\n")
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
