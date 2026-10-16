// CSSARIAJS is the tiny ARIA state synchroniser for CSS-signal scopes that need
// synthesized state the user agent does not maintain on a native control (tabs,
// listbox, disclosure/dialog/popover). It is injected ONLY when such a role is
// used, so every other CSS-signal page stays fully zero-JS.
// It derives aria-selected / aria-expanded from the checked state of the native
// radio/checkbox each trigger labels - the same state the `:has()` stylesheet
// already tracks - so there is no per-interaction bookkeeping.
package renderer

// CSSARIAJS is the minified synchroniser (one delegated change listener plus an
// initial pass). Keep it small: it ships inline on ARIA-role pages only.
const CSSARIAJS = `(function(){function s(){document.querySelectorAll("[role=tab][for],[role=option][for],[aria-expanded][for]").forEach(function(l){var i=document.getElementById(l.getAttribute("for"));if(!i)return;var v=i.checked?"true":"false";if(l.hasAttribute("aria-selected"))l.setAttribute("aria-selected",v);if(l.hasAttribute("aria-expanded"))l.setAttribute("aria-expanded",v)})}document.addEventListener("change",s,true);if(document.readyState!=="loading")s();else document.addEventListener("DOMContentLoaded",s)})();`
