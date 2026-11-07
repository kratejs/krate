(function(){
if(window.__krateAria)return;
window.__krateAria=1;
var D=document,last=null,seq=0,queued=0;
function $(s,r){return Array.prototype.slice.call((r||D).querySelectorAll(s))}
function vis(e){return e.getClientRects().length>0&&getComputedStyle(e).visibility!=="hidden"}
function shown(m){
var a=m.closest("[data-kz]"),x=a&&a.querySelector("input[data-ka=x]");
return vis(m)&&!(x&&x.checked);
}
function foc(r){return $('a[href],button:not([disabled]),input:not([disabled]):not([aria-hidden=true]):not([type=hidden]),select,textarea,summary,[tabindex]:not([tabindex="-1"])',r).filter(vis)}
function lab(e){return e&&e.closest?e.closest("label[data-ka]"):null}
function ctl(l){return D.getElementById(l.getAttribute("for"))}
function sync(root){
$("label[data-ka]",root).forEach(function(l){
var m=l.getAttribute("data-ka"),r=l.getAttribute("role"),i=ctl(l),on=!!(i&&i.checked);
if(m==="a"){l.setAttribute("aria-expanded","true")}
else if(m==="t"){
if(r==="tab"||r==="option")l.setAttribute("aria-selected",on?"true":"false");
if(l.hasAttribute("aria-expanded"))l.setAttribute("aria-expanded",on?"true":"false");
}
if(l.getAttribute("aria-disabled")==="true"){l.setAttribute("tabindex","-1");return}
l.setAttribute("tabindex",(r==="tab"||r==="option")?(on?"0":"-1"):(r==="menuitem"?"-1":"0"));
});
}
function link(){
$("[role=dialog],[role=alertdialog]").forEach(function(d){
[["title","aria-labelledby"],["desc","aria-describedby"]].forEach(function(p){
var t=d.querySelector("[data-ka="+p[0]+"]");
if(!t||d.hasAttribute(p[1]))return;
if(!t.id)t.id="krk"+(++seq);
d.setAttribute(p[1],t.id);
});
});
}
function enter(p){
var f=p.getAttribute("role")==="menu"?$("[role=menuitem]",p).filter(vis):foc(p);
var t=f[0]||p;
t.focus();
return t;
}
function activate(l){last=l;l.click()}
function move(list,from,k){
var n=list.length,i=list.indexOf(from);
if(!n)return null;
if(k==="Home")i=0;else if(k==="End")i=n-1;else i=(i+(k==="next"?1:-1)+n)%n;
return list[i];
}
var mdl=null,mdp="",pd=null;
function inertAround(m,on){
for(var n=m;n&&n!==D.body&&n.parentNode;n=n.parentNode){
var ps=n.parentNode.children;
for(var i=0;i<ps.length;i++){
var s=ps[i];
if(s===n||/^(SCRIPT|STYLE|LINK|TEMPLATE|INPUT)$/.test(s.tagName))continue;
if(on){if(!s.hasAttribute("inert")){s.setAttribute("inert","");s.setAttribute("data-kinert","")}}
else if(s.hasAttribute("data-kinert")){s.removeAttribute("inert");s.removeAttribute("data-kinert")}
}
}
}
function modal(){
var m=$("[aria-modal=true]").filter(shown)[0]||null;
if(m===mdl)return;
if(mdl){inertAround(mdl,false);D.documentElement.style.overflow=mdp}
mdl=m;
if(m){mdp=D.documentElement.style.overflow;D.documentElement.style.overflow="hidden";inertAround(m,true)}
}
D.addEventListener("pointerdown",function(e){pd=e.target},true);
D.addEventListener("click",function(e){
var m=mdl,t=e.target,down=pd;
pd=null;
if(!m||m.getAttribute("role")==="alertdialog"||!t||t.tagName==="INPUT"||m.contains(t)||(down&&m.contains(down)))return;
var a=m.closest("[data-kz]"),x=a&&a.querySelector("input[data-ka=x]");
if(x&&!x.checked){e.preventDefault();e.stopPropagation();x.click()}
},true);
function owner(a){return $("label[data-ka=a]",a).filter(vis)[0]}
D.addEventListener("click",function(e){var l=lab(e.target);if(l)last=l},true);
function hid(e){return!!e&&e.tagName==="INPUT"&&e.getAttribute("aria-hidden")==="true"}
D.addEventListener("change",function(e){
var i=e.target,a=i.closest?i.closest("[data-kz]"):null;
if(!a||i.tagName!=="INPUT")return;
var act=D.activeElement,from=lab(act)||last,tgt=null;
var lost=!act||act===D.body||hid(act)||!vis(act);
last=null;
sync(a);
modal();
if(i.getAttribute("data-ka")==="x"){
if(!i.checked)return;
var t=$("label[data-ka=t]",a).filter(function(l){return l.getAttribute("for")===a._o})[0];
if(t&&vis(t)&&lost)tgt=t;
}else if(i.checked&&a.querySelector("input[data-ka=x]")){
a._o=i.id;
var pid=from&&from.getAttribute("aria-controls"),p=pid&&D.getElementById(pid);
if(p&&/^(dialog|alertdialog|menu)$/.test(p.getAttribute("role")||"")){tgt=enter(p)}
else if(from&&lost){
var alt=from.nextElementSibling;
if(alt&&alt.getAttribute("data-ka")==="a"&&vis(alt))tgt=alt;
}
}else if(from&&vis(from)){tgt=from}
if(tgt&&tgt.ownerDocument.activeElement!==tgt)tgt.focus();
var sx=a.querySelector("input[data-ka=x]");
if(sx&&i.checked){
var d={open:!sx.checked,value:sx.checked?"":i.value};
a.dispatchEvent(new CustomEvent("krateopenchange",{detail:d}));
$("[data-kzo]",a).filter(function(b){return b.closest("[data-kz]")===a}).forEach(function(b){b.dispatchEvent(new CustomEvent("toggle",{detail:d}))});
}
setTimeout(function(){
if(!hid(D.activeElement))return;
var f=tgt&&vis(tgt)?tgt:(from&&vis(from)?from:null);
if(f)f.focus();
},0);
},true);
D.addEventListener("keydown",function(e){
var t=e.target,k=e.key,l=lab(t),p=t.closest?t.closest("[data-ka=p]"):null,a=t.closest?t.closest("[data-kz]"):null;
if(!a&&k==="Escape"){
var om=$("[aria-modal=true]").filter(shown)[0];
if(om){t=om;p=om;a=om.closest("[data-kz]")}
}
if(!a)return;
if(k==="Escape"){
var x=a.querySelector("input[data-ka=x]");
if(x&&!x.checked&&(p||hid(t)||(l&&l.getAttribute("data-ka")==="a"))){e.preventDefault();x.click()}
return;
}
if(k==="Tab"){
var m=t.closest("[aria-modal=true]");
if(!m)return;
var f=foc(m);
if(!f.length){e.preventDefault();m.focus();return}
if(e.shiftKey&&(t===f[0]||t===m)){e.preventDefault();f[f.length-1].focus()}
else if(!e.shiftKey&&t===f[f.length-1]){e.preventDefault();f[0].focus()}
return;
}
if(!l||l!==t)return;
var r=l.getAttribute("role"),mk=l.getAttribute("data-ka");
if(k==="Enter"||k===" "){e.preventDefault();activate(l);return}
var rtl=getComputedStyle(l).direction==="rtl",dir=k==="ArrowRight"?(rtl?"prev":"next"):k==="ArrowLeft"?(rtl?"next":"prev"):k==="ArrowDown"?"next":k==="ArrowUp"?"prev":(k==="Home"||k==="End")?k:"";
if(!dir)return;
var n;
if(r==="tab"||r==="option"){
if(k==="ArrowDown"&&r==="tab"||k==="ArrowUp"&&r==="tab")return;
n=move($("label[data-ka=t]",l.closest("[role=tablist],[role=listbox]")||a).filter(vis),l,dir);
if(n){e.preventDefault();n.focus();n.click()}
}else if(r==="menuitem"&&mk==="c"){
var pm=l.closest("[role=menu]");
if(k==="ArrowLeft"||k==="ArrowRight"){
var o=owner(a),hs=$("label[aria-haspopup=menu][data-ka=a],label[aria-haspopup=menu][data-ka=t]",a).filter(vis);
if(o&&a.getAttribute("data-kz")==="menubar"){var nx=move(hs,o,dir);if(nx){e.preventDefault();activate(nx)}}
return;
}
n=move($("label[data-ka=c]",pm||a).filter(vis),l,dir);
if(n){e.preventDefault();n.focus()}
}else if(r==="button"&&l.getAttribute("aria-haspopup")==="menu"){
if(a.getAttribute("data-kz")==="menubar"&&(k==="ArrowLeft"||k==="ArrowRight")){
var hb=$("label[aria-haspopup=menu][data-ka=a],label[aria-haspopup=menu][data-ka=t]",a).filter(vis);
n=move(hb,l,dir);
if(n){e.preventDefault();if(mk==="a")activate(n);else n.focus()}
}else if(k==="ArrowDown"&&mk==="t"){e.preventDefault();activate(l)}
}
},false);
D.addEventListener("focusin",function(e){
var m=$("[aria-modal=true]").filter(vis)[0];
if(m&&!m.contains(e.target))enter(m);
},true);
function drive(el){
var a=el.closest("[data-kz]"),x=a&&a.querySelector("input[data-ka=x]"),w=el.getAttribute("data-kopen");
if(!x)return;
if(w==="open"&&x.checked){var r=$("input",a).filter(function(n){return n.type==="radio"&&n!==x&&n.closest("[data-kz]")===a})[0];if(r)r.click()}
else if(w==="closed"&&!x.checked)x.click();
}
function init(){sync(D);link();modal()}
function first(){
init();
$("[data-kopen]").forEach(drive);
var m=$("[aria-modal=true]").filter(shown)[0];
if(m&&!m.contains(D.activeElement))enter(m);
}
if(window.MutationObserver){
new MutationObserver(function(ms){ms.forEach(function(m){if(m.target.getAttribute("data-kopen"))drive(m.target)})}).observe(D.documentElement,{attributes:true,attributeFilter:["data-kopen"],subtree:true});
new MutationObserver(function(){
if(queued)return;
queued=1;
requestAnimationFrame(function(){queued=0;init()});
}).observe(D.documentElement,{childList:true,subtree:true});
}
if(D.readyState!=="loading")first();else D.addEventListener("DOMContentLoaded",first);
})();
