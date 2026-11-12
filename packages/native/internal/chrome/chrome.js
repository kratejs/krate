(function () {
  "use strict";
  if (window.__krateChrome) return;
  var BASE = "/__krate/chrome";
  var C = (window.__krateChrome = {});
  var info = { native: false, edges: false };
  var rules = [];
  var edgeEls = [];

  function send(op, extra) {
    var body = { op: op };
    if (extra) for (var k in extra) body[k] = extra[k];
    try {
      return fetch(BASE, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body),
        keepalive: true,
      }).catch(function () {});
    } catch (e) {
      return null;
    }
  }

  // parseCSS collects the -webkit-app-region and app-region declarations of a
  // style sheet. Browsers other than WebView2 drop the property while parsing,
  // so the page's own style sheets are read as text.
  var DECL = /(?:^|[;\s{])(?:-webkit-)?app-region\s*:\s*(drag|no-drag|none)\s*(?:!important\s*)?(?=;|$)/g;
  function parseCSS(css, out) {
    css = css.replace(/\/\*[\s\S]*?\*\//g, "");
    if (css.indexOf("app-region") < 0) return;
    var i = 0, n = css.length;
    while (i < n) {
      var j = i;
      while (j < n && css.charAt(j) !== "{" && css.charAt(j) !== ";" && css.charAt(j) !== "}") j++;
      if (j >= n) return;
      var prelude = css.slice(i, j).trim();
      if (css.charAt(j) !== "{") {
        i = j + 1;
        continue;
      }
      var depth = 1, k = j + 1;
      while (k < n && depth > 0) {
        var ch = css.charAt(k);
        if (ch === "{") depth++;
        else if (ch === "}") depth--;
        k++;
      }
      var body = css.slice(j + 1, k - 1);
      i = k;
      if (prelude.charAt(0) === "@") {
        var m = /^@(media|supports|layer|container)\b\s*([\s\S]*)$/.exec(prelude);
        if (!m) continue;
        var ok = true;
        if (m[1] === "media") {
          try {
            ok = window.matchMedia(m[2]).matches;
          } catch (e) {
            ok = false;
          }
        }
        if (ok) parseCSS(body, out);
        continue;
      }
      if (!prelude) continue;
      var v = "", d;
      DECL.lastIndex = 0;
      while ((d = DECL.exec(body))) v = d[1];
      if (v) out.push({ sel: prelude, v: v });
    }
  }

  function scan() {
    var out = [], pending = 1;
    function fin() {
      if (--pending === 0) rules = out;
    }
    var styles = document.querySelectorAll("style");
    for (var s = 0; s < styles.length; s++) parseCSS(styles[s].textContent || "", out);
    var links = document.querySelectorAll('link[rel~="stylesheet"]');
    for (var l = 0; l < links.length; l++) {
      pending++;
      try {
        fetch(links[l].href)
          .then(function (r) { return r.text(); })
          .then(function (t) { parseCSS(t, out); })
          .catch(function () {})
          .then(fin);
      } catch (e) {
        fin();
      }
    }
    fin();
  }

  var INLINE = /(?:^|[;\s])(?:-webkit-)?app-region\s*:\s*(drag|no-drag|none)/;

  // explicit returns the region an element declares itself: "drag", "no-drag"
  // or "".
  function explicit(el) {
    var a = el.getAttribute("data-krate-drag");
    if (a !== null) return a === "false" || a === "no-drag" ? "no-drag" : "drag";
    if (info.native) return "";
    var st = el.getAttribute("style");
    var v = "";
    if (st && st.indexOf("app-region") >= 0) {
      var m = INLINE.exec(st);
      if (m && m[1] !== "none") return m[1];
    }
    for (var r = 0; r < rules.length; r++) {
      try {
        if (el.matches(rules[r].sel) && rules[r].v !== "none") v = rules[r].v;
      } catch (e) {}
    }
    if (v) return v;
    try {
      var p = getComputedStyle(el).getPropertyValue("--krate-app-region").trim();
      if (p === "drag" || p === "no-drag") return p;
    } catch (e) {}
    return "";
  }

  var INTERACTIVE = /^(A|BUTTON|INPUT|SELECT|TEXTAREA|LABEL|SUMMARY|OPTION|VIDEO|AUDIO)$/;

  // regionOf is the region at an element: the nearest ancestor that declares
  // one. Controls inside a drag region stay clickable.
  function regionOf(el) {
    for (var n = el; n && n.nodeType === 1; n = n.parentElement) {
      if (INTERACTIVE.test(n.tagName) || n.isContentEditable) {
        if (explicit(n) !== "drag") return "no-drag";
      }
      var v = explicit(n);
      if (v) return v;
    }
    return "";
  }

  // A native drag swallows the release of the first press, so the page may
  // never see a dblclick; two presses close together count as one instead.
  var last = { t: 0, x: 0, y: 0 };
  addEventListener(
    "mousedown",
    function (e) {
      if (e.button !== 0 || e.ctrlKey || !(e.target && e.target.nodeType === 1)) return;
      if (regionOf(e.target) !== "drag") return;
      var now = Date.now();
      var dbl = now - last.t < 450 && Math.abs(e.screenX - last.x) < 5 && Math.abs(e.screenY - last.y) < 5;
      last = dbl ? { t: 0, x: 0, y: 0 } : { t: now, x: e.screenX, y: e.screenY };
      e.preventDefault();
      send(dbl ? "toggleMaximize" : "drag");
    },
    true
  );

  // Frameless windows on Linux get no resize border from the window manager,
  // so the page carries thin transparent edges that start a native resize.
  var CURSORS = {
    topLeft: "nwse-resize", top: "ns-resize", topRight: "nesw-resize", left: "ew-resize",
    right: "ew-resize", bottomLeft: "nesw-resize", bottom: "ns-resize", bottomRight: "nwse-resize",
  };
  function place(el, name, t) {
    var s = el.style;
    s.position = "fixed";
    s.zIndex = "2147483647";
    s.background = "transparent";
    s.cursor = CURSORS[name];
    s.width = /left|right/i.test(name) ? t + "px" : "calc(100% - " + 2 * t + "px)";
    s.height = /top|bottom/i.test(name) ? t + "px" : "calc(100% - " + 2 * t + "px)";
    s.left = /left/i.test(name) ? "0" : /right/i.test(name) ? "" : t + "px";
    s.right = /right/i.test(name) ? "0" : "";
    s.top = /top/i.test(name) ? "0" : /bottom/i.test(name) ? "" : t + "px";
    s.bottom = /bottom/i.test(name) ? "0" : "";
    if (/^(top|bottom)(Left|Right)$/.test(name)) {
      s.width = t + "px";
      s.height = t + "px";
    }
  }
  function setEdges(on) {
    if (on && !edgeEls.length) {
      Object.keys(CURSORS).forEach(function (name) {
        var el = document.createElement("div");
        el.setAttribute("data-krate-edge", name);
        place(el, name, name.length > 6 && /Left|Right/.test(name) ? 8 : 5);
        el.addEventListener("mousedown", function (e) {
          if (e.button !== 0) return;
          e.preventDefault();
          e.stopPropagation();
          send("resize", { edge: name });
        });
        (document.body || document.documentElement).appendChild(el);
        edgeEls.push(el);
      });
    }
    for (var i = 0; i < edgeEls.length; i++) edgeEls[i].style.display = on ? "" : "none";
  }

  var timer = 0;
  function refresh() {
    clearTimeout(timer);
    timer = setTimeout(function () {
      try {
        fetch(BASE, { headers: { accept: "application/json" } })
          .then(function (r) { return r.json(); })
          .then(function (j) {
            info = { native: !!j.nativeRegions, edges: !!j.edges };
            setEdges(info.edges);
          })
          .catch(function () {});
      } catch (e) {}
    }, 30);
  }

  function start() {
    scan();
    refresh();
    try {
      new MutationObserver(function (muts) {
        for (var i = 0; i < muts.length; i++) {
          var added = muts[i].addedNodes;
          for (var j = 0; j < added.length; j++) {
            var nd = added[j];
            if (nd.nodeType === 1 && (nd.tagName === "STYLE" || nd.tagName === "LINK")) return scan();
          }
        }
      }).observe(document.documentElement, { childList: true, subtree: true });
    } catch (e) {}
  }
  addEventListener("resize", refresh);
  addEventListener("focus", refresh);
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", start);
  else start();

  C.version = 1;
  C.regionOf = regionOf;
  C._parse = function (css) {
    var out = [];
    parseCSS(css, out);
    return out;
  };
  C._setInfo = function (i) { info = i; };
  C._setRules = function (r) { rules = r; };
})();
