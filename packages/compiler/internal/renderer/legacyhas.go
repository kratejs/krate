// LegacyHasJS is the scoped shim that drives the class-based stylesheet emitted
// in legacy/hybrid compat modes. Browsers without :has() cannot express "scope
// root whose controller input is checked" in CSS, so this script mirrors each
// hidden controller input's checked state onto its scope root as a
// `<token>-on` class (and the focused input's trigger label as `krc-f`). The
// legacy stylesheet keys off those classes instead of :has().
//
// It is a no-op where :has() is supported (hybrid mode guards the injection
// with a feature query) and idempotent via window.__krateHasShim, so duplicate
// injection is harmless. Keep it small: it ships on legacy CSS-signal pages.
package renderer

// LegacyHasJS is the minified shim: one initial pass, delegated
// change/input/focus listeners, and a MutationObserver so state is also synced
// for markup inserted after load (SPA/router swaps). State changes resync only
// the affected scope root (not the whole document).
const LegacyHasJS = `(function(){if(window.__krateHasShim)return;window.__krateHasShim=1;function t(i){var l=i.classList;for(var k=0;k<l.length;k++){if(l[k]!=="krc-h"&&l[k].indexOf("krc")===0)return l[k]}return ""}function u(i){var c=t(i);if(!c)return;var r=i.closest("."+c.split("-")[0]);if(r)r.classList.toggle(c+"-on",!!i.checked)}function n(list){for(var k=0;k<list.length;k++)u(list[k])}function all(){n(document.querySelectorAll("input.krc-h"))}function one(i){var c=t(i);if(!c)return;var r=i.closest("."+c.split("-")[0]);if(r)n(r.querySelectorAll("input.krc-h"))}function f(e,on){var i=e.target;if(!i||!i.classList||!i.classList.contains("krc-h")||!i.id)return;var l=i.ownerDocument.querySelector('label[for="'+i.id.replace(/"/g,"")+'"]');if(l)l.classList.toggle("krc-f",on)}function added(x){if(!x||x.nodeType!==1)return;if(x.classList&&x.classList.contains("krc-h")){u(x);return}if(x.querySelectorAll){var ins=x.querySelectorAll("input.krc-h");if(ins.length)n(ins)}}document.addEventListener("change",function(e){one(e.target)},true);document.addEventListener("input",function(e){one(e.target)},true);document.addEventListener("focusin",function(e){f(e,true)},true);document.addEventListener("focusout",function(e){f(e,false)},true);if(window.MutationObserver){new MutationObserver(function(ms){for(var i=0;i<ms.length;i++){var a=ms[i].addedNodes;for(var j=0;j<a.length;j++)added(a[j])}}).observe(document.documentElement,{childList:true,subtree:true})}if(document.readyState!=="loading")all();else document.addEventListener("DOMContentLoaded",all)})();`
