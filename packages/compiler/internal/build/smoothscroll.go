package build

import (
	_ "embed"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

//go:embed assets/lenis.min.js
var lenisLibJS string

//go:embed assets/lenis-snap.min.js
var lenisSnapJS string

//go:embed assets/lenis.css
var lenisCSS string

type lenisInitConfig struct {
	Options map[string]any `json:"options"`
	Snap    map[string]any `json:"snap,omitempty"`
}

func setFloat(m map[string]any, key string, v *float64) {
	if v != nil {
		m[key] = *v
	}
}

func setBool(m map[string]any, key string, v *bool) {
	if v != nil {
		m[key] = *v
	}
}

func setString(m map[string]any, key, v string) {
	if v != "" {
		m[key] = v
	}
}

func setTrue(m map[string]any, key string, v bool) {
	if v {
		m[key] = true
	}
}

func lenisInitFor(c *config.SmoothScrollConfig) lenisInitConfig {
	o := map[string]any{}
	setFloat(o, "lerp", c.Lerp)
	setFloat(o, "duration", c.Duration)
	setString(o, "easing", c.Easing)
	setBool(o, "smoothWheel", c.SmoothWheel)
	setTrue(o, "syncTouch", c.SyncTouch)
	setFloat(o, "syncTouchLerp", c.SyncTouchLerp)
	setFloat(o, "touchInertiaExponent", c.TouchInertiaExponent)
	setTrue(o, "infinite", c.Infinite)
	setString(o, "orientation", c.Orientation)
	setString(o, "gestureOrientation", c.GestureOrientation)
	setFloat(o, "touchMultiplier", c.TouchMultiplier)
	setFloat(o, "wheelMultiplier", c.WheelMultiplier)
	setBool(o, "autoResize", c.AutoResize)
	setBool(o, "overscroll", c.Overscroll)
	setBool(o, "autoRaf", c.AutoRaf)
	setTrue(o, "autoToggle", c.AutoToggle)
	setTrue(o, "allowNestedScroll", c.AllowNestedScroll)
	setTrue(o, "naiveDimensions", c.NaiveDimensions)
	setTrue(o, "stopInertiaOnNavigate", c.StopInertiaOnNavigate)
	setBool(o, "respectReducedMotion", c.RespectReducedMotion)
	setString(o, "prevent", c.Prevent)
	setString(o, "wrapper", c.Wrapper)
	setString(o, "content", c.Content)
	setString(o, "eventsTarget", c.EventsTarget)
	if a := c.Anchors; a != nil && a.Enabled {
		am := map[string]any{}
		setFloat(am, "offset", a.Offset)
		setFloat(am, "duration", a.Duration)
		setString(am, "easing", a.Easing)
		setTrue(am, "immediate", a.Immediate)
		setTrue(am, "lock", a.Lock)
		if len(am) == 0 {
			o["anchors"] = true
		} else {
			o["anchors"] = am
		}
	}
	out := lenisInitConfig{Options: o}
	if s := c.Snap; s != nil && s.Enabled {
		sm := map[string]any{}
		setString(sm, "type", s.Type)
		setFloat(sm, "lerp", s.Lerp)
		setFloat(sm, "duration", s.Duration)
		setString(sm, "easing", s.Easing)
		setString(sm, "distanceThreshold", s.DistanceThreshold)
		if s.Debounce != nil {
			sm["debounce"] = *s.Debounce
		}
		out.Snap = sm
	}
	return out
}

const lenisInitJS = `(function(){
if(window.lenis||typeof Lenis==="undefined")return;
var C=__LENIS_CONFIG__;
var P={
linear:function(t){return t},
easeInQuad:function(t){return t*t},
easeOutQuad:function(t){return t*(2-t)},
easeInOutQuad:function(t){return t<.5?2*t*t:-1+(4-2*t)*t},
easeInCubic:function(t){return t*t*t},
easeOutCubic:function(t){return 1-Math.pow(1-t,3)},
easeInOutCubic:function(t){return t<.5?4*t*t*t:1-Math.pow(-2*t+2,3)/2},
easeInQuart:function(t){return t*t*t*t},
easeOutQuart:function(t){return 1-Math.pow(1-t,4)},
easeInOutQuart:function(t){return t<.5?8*t*t*t*t:1-Math.pow(-2*t+2,4)/2},
easeInExpo:function(t){return t===0?0:Math.pow(2,10*t-10)},
easeOutExpo:function(t){return Math.min(1,1.001-Math.pow(2,-10*t))},
easeInOutExpo:function(t){return t===0?0:t===1?1:t<.5?Math.pow(2,20*t-10)/2:(2-Math.pow(2,-20*t+10))/2},
easeInSine:function(t){return 1-Math.cos(t*Math.PI/2)},
easeOutSine:function(t){return Math.sin(t*Math.PI/2)},
easeInOutSine:function(t){return -(Math.cos(Math.PI*t)-1)/2}
};
function bezier(x1,y1,x2,y2){
function ax(t){return 3*x1*(1-t)*(1-t)*t+3*x2*(1-t)*t*t+t*t*t}
function ay(t){return 3*y1*(1-t)*(1-t)*t+3*y2*(1-t)*t*t+t*t*t}
return function(x){
if(x<=0)return 0;
if(x>=1)return 1;
var lo=0,hi=1,t=x;
for(var i=0;i<32;i++){
var v=ax(t);
if(Math.abs(v-x)<1e-6)break;
if(v<x)lo=t;else hi=t;
t=(lo+hi)/2
}
return ay(t)
}
}
function easing(v){
if(P[v])return P[v];
var m=/^cubic-bezier\(([^)]+)\)$/.exec(v||"");
if(!m)return undefined;
var p=m[1].split(",").map(Number);
return bezier(p[0],p[1],p[2],p[3])
}
function pick(sel){return sel?document.querySelector(sel):null}
function resolve(src){
var o={};
for(var k in src)o[k]=src[k];
if(o.easing){var e=easing(o.easing);if(e)o.easing=e;else delete o.easing}
return o
}
var o=resolve(C.options);
if(o.anchors&&typeof o.anchors==="object")o.anchors=resolve(o.anchors);
["wrapper","content","eventsTarget"].forEach(function(k){
if(o[k]){var n=pick(o[k]);if(n)o[k]=n;else delete o[k]}
});
if(o.prevent){
var sel=o.prevent;
o.prevent=function(n){return !!(n&&n.closest&&n.closest(sel))}
}
if(o.autoRaf===undefined)o.autoRaf=true;
var l=new Lenis(o);
window.lenis=l;
if(C.snap&&typeof Snap!=="undefined"){
var so=resolve(C.snap);
if(so.distanceThreshold&&so.distanceThreshold.slice(-1)!=="%")so.distanceThreshold=Number(so.distanceThreshold);
var s=new Snap(l,so);
window.lenisSnap=s;
var removers=[];
var sync=function(){
removers.forEach(function(r){r()});
removers=[];
document.querySelectorAll("[data-lenis-snap]").forEach(function(el){
var a=el.getAttribute("data-lenis-snap");
removers.push(s.addElement(el,a?{align:a.split(/[\s,]+/).filter(Boolean)}:{}))
})
};
sync();
window.addEventListener("krate:navigate-end",function(){requestAnimationFrame(sync)})
}
})();`

func lenisChunkSource(c *config.SmoothScrollConfig, minify bool) (string, error) {
	data, err := json.Marshal(lenisInitFor(c))
	if err != nil {
		return "", err
	}
	init := strings.Replace(lenisInitJS, "__LENIS_CONFIG__", string(data), 1)
	if minify {
		init = minifyJSBase(init)
	}
	var sb strings.Builder
	sb.WriteString(lenisLibJS)
	sb.WriteString("\n")
	if c.Snap != nil && c.Snap.Enabled {
		sb.WriteString(lenisSnapJS)
		sb.WriteString("\n")
	}
	sb.WriteString(init)
	sb.WriteString("\n")
	return sb.String(), nil
}

func lenisHeadHTML(basePath, chunk string) string {
	if chunk == "" {
		return ""
	}
	return "<style>" + strings.TrimSpace(lenisCSS) + "</style>\n" +
		"<script defer src=\"" + assetHref(basePath, chunk) + "\"></script>\n"
}

func (b *Builder) writeSmoothScrollChunk() error {
	b.lenisChunk = ""
	if !b.Cfg.SmoothScrollEnabled() {
		return nil
	}
	src, err := lenisChunkSource(b.Cfg.SmoothScroll, b.shouldMinifyJS())
	if err != nil {
		return err
	}
	rel := "chunks/lenis." + hashContent([]byte(src)) + ".js"
	if err := writeOutput(filepath.Join(b.Cfg.OutDir, rel), []byte(src)); err != nil {
		return err
	}
	b.lenisChunk = rel
	return nil
}
