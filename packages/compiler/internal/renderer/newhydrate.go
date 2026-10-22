package renderer

import (
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/htmlspec"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// HydrationBootstrapJS is the shared hydration infrastructure appended to the
// runtime chunk instead of being emitted into every page's JS. It defines the
// slot lookup helpers, the XSS sanitizer, and the compact binding helpers
// (kbind*) that per-page hydration scripts call.
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
var __tpls=new Map();
function __dec(v){try{return decodeURIComponent(v);}catch(e){return v;}}
function scanIn(r){var w=document.createTreeWalker(r,NodeFilter.SHOW_COMMENT,null,false);while(w.nextNode()){var v=w.currentNode.nodeValue;if(v.indexOf('k:')===0)__cache.set(__dec(v.slice(2)),w.currentNode);}}
function scan(){__cache.clear();__tpls.clear();if(typeof krateCondReset==='function')krateCondReset();scanIn(__root);var ts=__root.querySelectorAll('template[data-kc]');for(var i=0;i<ts.length;i++){__tpls.set(ts[i].getAttribute('data-kc'),ts[i]);scanIn(ts[i].content);}}
scan();
window.findSlot=function(id){var q='[data-k="k:'+id+'"]';var el=__root.querySelector(q);if(el)return el;var c=__cache.get(id);if(c)return c;var found=null;__tpls.forEach(function(t){if(!found)found=t.content.querySelector(q);});if(!found&&typeof krateCondDetached==='function'){var ds=krateCondDetached();for(var i=0;i<ds.length&&!found;i++)found=ds[i].querySelector(q);}return found;};
window.refreshSlots=scan;
window.krateMountPortals=function(){var list=document.querySelectorAll('[data-krate-portal]');for(var i=0;i<list.length;i++){(function(node){if(node.getAttribute('data-krate-portal-mounted'))return;node.setAttribute('data-krate-portal-mounted','1');if(node.parentNode!==document.body)document.body.appendChild(node);if(typeof window.onCleanup==='function')window.onCleanup(function(){if(node.parentNode)node.parentNode.removeChild(node);});})(list[i]);}};
window.kbindText=function(id,get){var n=findSlot(id);if(!n)return;__safe(function(){createEffect(function(){var v=get();if(v==null||v===false)v='';if(n.nextSibling&&n.nextSibling.nodeType===3){n.nextSibling.textContent=String(v);}else{var t=document.createTextNode(String(v));n.parentNode.insertBefore(t,n.nextSibling);}});});};
window.kbindContent=function(id,get){var n=findSlot(id);if(!n)return;var __first=true;__safe(function(){createEffect(function(){var v=get();if(__first){__first=false;if(typeof krateSlotReplaced==='function')krateSlotReplaced(id);}while(n.nextSibling&&!(n.nextSibling.nodeType===8&&n.nextSibling.nodeValue==='/k:'+n.nodeValue.slice(2))){var _d=n.nextSibling;n.parentNode.removeChild(_d);if(typeof disposeNode==='function')disposeNode(_d);}if(v==null||v===false)v='';if(typeof v==='string'||typeof v==='number')v=document.createTextNode(String(v));if(Array.isArray(v)){var ref=n.nextSibling;for(var i=0;i<v.length;i++)n.parentNode.insertBefore(v[i],ref);}else n.parentNode.insertBefore(v,n.nextSibling);});});};
window.kbindCond=function(id,get){var n=findSlot(id);if(!n)return;var t=__tpls.get(id)||null;__safe(function(){createEffect(function(){var s=!!get();krateCondSwap(n,s,t);if(typeof krateCondActivity==='function')krateCondActivity(id,s?'c':'a');});});};
window.kbindAttr=function(id,attr,get){var n=findSlot(id);if(!n)return;__safe(function(){createEffect(function(){var v=get();if(v==null||v===false)n.removeAttribute(attr);else if(v===true)n.setAttribute(attr,'');else if(attr==='style'&&typeof v==='object'){n.removeAttribute('style');Object.assign(n.style,v);}else n.setAttribute(attr,krateSanitizeAttr(attr,String(v)));});});};
window.kbindProp=function(id,attr,get){var n=findSlot(id);if(!n)return;__safe(function(){createEffect(function(){var v=get();if(attr==='value'){var s=v==null||v===false?'':String(v);if(n.value===s)return;n.value=s;}else{var b=!!v;if(n[attr]===b)return;n[attr]=b;}});});};
window.kbindHandler=function(id,prop,fn){if(typeof fn!=='function')return;var n=findSlot(id);if(n)__safe(function(){n[prop]=fn;});};
window.kbindEvent=function(id,ev,fn,capture){var n=findSlot(id);if(n)__safe(function(){n.addEventListener(ev,fn,!!capture);});};
window.kbindRef=function(id,set){var n=findSlot(id);if(n)__safe(function(){set(n);});};
function __krateActionBase(){var c=window.__KRATE_CFG__;return c&&c.basePath?c.basePath:'';}
window.__krateAction=function(id){return function(){var args=Array.prototype.slice.call(arguments);return fetch(__krateActionBase()+'/__krate/action/'+id,{method:'POST',credentials:'same-origin',headers:{'content-type':'application/json','accept':'application/json','x-krate-action':'fetch','x-krate-return':location.pathname+location.search},body:JSON.stringify({args:args})}).then(function(r){return r.json().then(function(p){if(!r.ok)throw new Error(p&&p.details?p.details:p&&p.error?p.error:r.statusText);return p.result;});});};};
if(!window.__krateActionBound){window.__krateActionBound=true;document.addEventListener('submit',function(e){var form=e.target;if(!form||form.tagName!=='FORM'||e.defaultPrevented)return;var submitter=e.submitter;var actionEl=null;if(submitter&&submitter.hasAttribute&&submitter.hasAttribute('data-krate-action'))actionEl=submitter;else if(form.hasAttribute('data-krate-action'))actionEl=form;if(!actionEl)return;if(submitter&&submitter.hasAttribute('formaction')&&!submitter.hasAttribute('data-krate-action'))return;e.preventDefault();if(form.getAttribute('data-krate-action-pending'))return;form.setAttribute('data-krate-action-pending','1');form.setAttribute('aria-busy','true');var url=actionEl===form?form.action:(submitter.formAction||form.action);var fd=new FormData(form);if(submitter&&submitter.name){fd.append(submitter.name,submitter.value);}fetch(url,{method:'POST',credentials:'same-origin',headers:{'accept':'application/json','x-krate-action':'fetch','x-krate-return':location.pathname+location.search},body:fd}).then(function(r){return r.json().then(function(p){if(!r.ok)throw new Error(p&&p.details?p.details:p&&p.error?p.error:r.statusText);return p;});}).then(function(p){if(p&&typeof p.redirect==='string'&&p.redirect){location.assign(p.redirect);return;}location.reload();}).catch(function(err){form.removeAttribute('data-krate-action-pending');form.removeAttribute('aria-busy');console.error('krate server action:',err);});},false);}
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
			len(sig.SlotBindings) > 0 || len(sig.AttrBindings) > 0 || len(sig.RefBindings) > 0 ||
			len(sig.IslandBoot) > 0 {
			hasWork = true
			break
		}
	}
	if !hasWork && !result.HasLinks && result.IslandBootJS == "" && !strings.Contains(result.HTML, "data-krate-portal") {
		return ""
	}

	var b strings.Builder
	b.WriteString("(function(){\n")
	// Rebuild the comment-marker cache against the current DOM (fresh page
	// load or SPA navigation).
	b.WriteString("refreshSlots();\n")
	b.WriteString("if(window.krateMountPortals)window.krateMountPortals();\n")

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

	// React effects need component boundaries (ordering, unmount, hidden
	// branches); declare an owner per component when any component has them.
	owners := pageNeedsOwners(result)
	if owners {
		b.WriteString("var __ko=typeof krateOwner==='function'?krateOwner:function(){};\n")
	}

	// Per-component scoped IIFEs
	for _, sig := range result.Signatures {
		if sig.Tier != irtree.TierClient {
			continue
		}
		// Label each scope with its component so the readable dev output can be
		// traced back to a source component. Stripped by minification in prod.
		b.WriteString("// component ")
		b.WriteString(string(sig.ComponentID))
		b.WriteString("\n(function(){\n")
		if owners {
			writeOwnerDecl(&b, sig)
		}

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

		// External island prop providers. These must run inside this component's
		// IIFE: the props read its signal getters/setters directly.
		for _, js := range sig.IslandBoot {
			b.WriteString(js)
			b.WriteByte('\n')
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
	if owners {
		b.WriteString("__ko(null);\n")
	}

	// Publish page-scope signal values for external React islands. This runs
	// inside the page IIFE so the signal getters are in scope.
	if result.IslandBootJS != "" {
		b.WriteString(result.IslandBootJS)
		if !strings.HasSuffix(result.IslandBootJS, "\n") {
			b.WriteByte('\n')
		}
	}

	// Event delegation
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
			b.WriteString("_')===0&&typeof _n[_k]==='function'){_n[_k](e);break;}}if(e.cancelBubble)return;_n=_n.parentNode;}});\n")
		}
		b.WriteString("__krate_del_cleanup=function(){for(var i=0;i<__krate_del_fns.length;i++){__krate_del_root.removeEventListener(__krate_del_fns[i].ev,__krate_del_fns[i].fn);}__krate_del_fns=[];};\n")
	}

	// Router
	b.WriteString("if(typeof reinitRouter==='function'){reinitRouter();}else if(typeof initRouter==='function'){initRouter();}\n")

	b.WriteString("})();\n")

	return b.String()
}

// pageNeedsOwners reports whether any client component registers effects,
// in which case every component scope declares its owner.
func pageNeedsOwners(result *EmitResult) bool {
	for _, sig := range result.Signatures {
		if sig.Tier == irtree.TierClient && len(sig.Effects) > 0 {
			return true
		}
	}
	return false
}

// writeOwnerDecl emits the krateOwner call that opens a component scope:
// its id, enclosing component, conditional branches and list slot.
func writeOwnerDecl(b *strings.Builder, sig irtree.ComponentSignature) {
	b.WriteString("__ko(")
	b.WriteString(strconv.Quote(string(sig.ComponentID)))
	b.WriteString(",")
	if sig.ParentID != "" {
		b.WriteString(strconv.Quote(string(sig.ParentID)))
	} else {
		b.WriteString("null")
	}
	if len(sig.Conds) > 0 || sig.ListSlot != "" {
		b.WriteString(",[")
		for i, c := range sig.Conds {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString("[")
			b.WriteString(strconv.Quote(string(c.SlotID)))
			b.WriteString(",")
			b.WriteString(strconv.Quote(c.Branch))
			if c.InitiallyShown {
				b.WriteString(",1]")
			} else {
				b.WriteString(",0]")
			}
		}
		b.WriteString("]")
		if sig.ListSlot != "" {
			b.WriteString(",")
			b.WriteString(strconv.Quote(string(sig.ListSlot)))
		}
	}
	b.WriteString(");\n")
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
	return htmlspec.IsPropertyAttr(name)
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

// CompatBootstrapJS exposes the small set of React compatibility APIs that
// transpiled source components can reference directly in their hydration
// IIFEs (there is no module import inside the emitted IIFE). Compiled packages
// use the Preact compat chunks instead.
const CompatBootstrapJS = `
window.useImperativeHandle=function(ref,create,deps){if(!ref)return;function set(v){if(typeof ref==='function')ref(v);else if(ref&&typeof ref==='object')ref.current=v;}createEffectDeps(function(){set(create());return function(){set(null);};},typeof deps==='function'?deps:deps?function(){return deps;}:null,'layout');};
window.useSyncExternalStore=function(subscribe,getSnapshot){var s=createSignal(getSnapshot());createEffect(function(){var unsub=subscribe(function(){s[1](getSnapshot());});return function(){if(typeof unsub==='function')unsub();};});return s[0];};
window.useTransition=function(){var s=createSignal(false);function start(fn){s[1](true);try{fn();}finally{s[1](false);}}return [s[0],start];};
window.startTransition=function(fn){fn();};
window.useDeferredValue=function(v){return v;};
window.useDebugValue=function(){};
window.isValidElement=function(v){return (typeof Node!=='undefined'&&v instanceof Node)||(v&&typeof v==='object');};
window.Children={toArray:function(c){if(c==null||c===false||c===true)return [];if(Array.isArray(c)){var o=[];for(var i=0;i<c.length;i++){var a=window.Children.toArray(c[i]);for(var j=0;j<a.length;j++)o.push(a[j]);}return o;}return [c];},count:function(c){return window.Children.toArray(c).length;},only:function(c){return window.Children.toArray(c)[0];},map:function(c,fn){return window.Children.toArray(c).map(fn);},forEach:function(c,fn){var a=window.Children.toArray(c);for(var i=0;i<a.length;i++)fn(a[i],i);}};
window.cloneElement=function(el,props){if(el==null||el===false||el===true)return el;var n=(typeof Node!=='undefined'&&el instanceof Node)?el.cloneNode(true):el;if(props&&n&&n.nodeType===1){for(var k in props){if(k==='children')continue;var v=props[k];if(v==null||v===false)continue;if(k==='className'||k==='class'){n.setAttribute('class',String(v));}else if(k==='style'&&typeof v==='object'){for(var s in v)n.style[s]=v[s];}else if(k.indexOf('on')===0&&typeof v==='function'){n[k.toLowerCase()]=v;}else{n.setAttribute(k,String(v));}}}if(arguments.length>2){while(n.firstChild)n.removeChild(n.firstChild);for(var i=2;i<arguments.length;i++){var c=arguments[i];if(c==null||c===false||c===true)continue;n.appendChild(typeof Node!=='undefined'&&c instanceof Node?c:document.createTextNode(String(c)));}}return n;};
`
