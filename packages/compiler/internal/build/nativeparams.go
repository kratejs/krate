package build

// A native app serves one prerendered shell page for every URL of a dynamic
// route (see NativeFallback), so nothing on the "server" side can substitute
// the build-time param sentinels the way `krate serve` does. This inline
// script does it in the webview instead: it matches the address bar against
// the route pattern in the page's __krate_page block, replaces each sentinel
// in text, attributes and the title, and publishes window.__KRATE_PARAMS__.
// It runs before the hydration scripts, and again after each soft navigation
// (krate:navigate fires once the URL has changed and before the new page's
// scripts run).
const nativeParamJS = `(function(){var P="__KRATE_PARAM_";function dec(s){try{return decodeURIComponent(s)}catch(e){return s}}` +
	`function fix(){var el=document.getElementById("__krate_page");if(!el)return;var route;try{route=JSON.parse(el.textContent).r}catch(e){return}` +
	`var pat=route.split("/").filter(Boolean),parts=location.pathname.split("/").filter(Boolean),p={};` +
	`for(var i=0;i<pat.length;i++){var m=/^\[(\[)?(\.\.\.)?([^\]]+)\]?\]$/.exec(pat[i]);if(!m)continue;` +
	`if(m[2]){p[m[3]]=parts.slice(i).map(dec);break}if(i<parts.length)p[m[3]]=dec(parts[i])}` +
	`var k=Object.keys(p);window.__KRATE_PARAMS__=p;if(!k.length)return;` +
	`function sub(s){if(s.indexOf(P)<0)return s;for(var j=0;j<k.length;j++){var v=p[k[j]];s=s.split(P+k[j]+"__").join(Array.isArray(v)?v.join("/"):v)}return s}` +
	`var w=document.createTreeWalker(document.documentElement,5),n;while(n=w.nextNode()){if(n.nodeType===3){var t=sub(n.nodeValue);if(t!==n.nodeValue)n.nodeValue=t}` +
	`else{var a=n.attributes;for(var q=0;q<a.length;q++){var u=sub(a[q].value);if(u!==a[q].value)n.setAttribute(a[q].name,u)}}}}` +
	`fix();if(!window.__krateParamFix){window.__krateParamFix=1;window.addEventListener("krate:navigate",fix)}})();`

// nativeParamScript returns the inline script for a dynamic page of a native
// build, and "" for every other page.
func (b *Builder) nativeParamScript(page string) string {
	if !b.NativeTarget || !isDynamicRoute(page, b.Cfg.PagesDir) {
		return ""
	}
	return bootScriptOpen + nativeParamJS + "</script>"
}
