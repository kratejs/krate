(function () {
  if (typeof window === "undefined" || window.krateMotion || typeof Motion === "undefined") return;
  var M = Motion;
  var C = __MOTION_CONFIG__;
  var doc = document;
  var root = doc.documentElement;
  var BOUND = "data-motion-bound";
  var READY = "data-motion-ready";
  var SELECTOR = "[data-motion],[data-motion-initial],[data-motion-animate],[data-motion-hover],[data-motion-press],[data-motion-scroll-animate]";
  var TRANSFORM_KEY = /^(x|y|z|translate[XYZ]?|scale[XYZ]?|rotate[XYZ]?|skew[XY]?|perspective|originX|originY|originZ)$/;
  var REST = { opacity: 1, x: 0, y: 0, z: 0, scale: 1, scaleX: 1, scaleY: 1, rotate: 0, rotateX: 0, rotateY: 0, rotateZ: 0, skew: 0, skewX: 0, skewY: 0 };
  var BUILTIN = {
    fade: { initial: { opacity: 0 }, animate: { opacity: 1 } },
    "fade-up": { initial: { opacity: 0, y: 24 }, animate: { opacity: 1, y: 0 } },
    "fade-down": { initial: { opacity: 0, y: -24 }, animate: { opacity: 1, y: 0 } },
    "fade-left": { initial: { opacity: 0, x: 24 }, animate: { opacity: 1, x: 0 } },
    "fade-right": { initial: { opacity: 0, x: -24 }, animate: { opacity: 1, x: 0 } },
    "zoom-in": { initial: { opacity: 0, scale: 0.92 }, animate: { opacity: 1, scale: 1 } },
    "zoom-out": { initial: { opacity: 0, scale: 1.08 }, animate: { opacity: 1, scale: 1 } },
    "blur-in": { initial: { opacity: 0, filter: "blur(8px)" }, animate: { opacity: 1, filter: "blur(0px)" } },
    "slide-up": { initial: { y: 48 }, animate: { y: 0 } },
    "slide-down": { initial: { y: -48 }, animate: { y: 0 } },
    "slide-left": { initial: { x: 48 }, animate: { x: 0 } },
    "slide-right": { initial: { x: -48 }, animate: { x: 0 } },
    "flip-x": { initial: { opacity: 0, rotateX: 80 }, animate: { opacity: 1, rotateX: 0 } },
    "flip-y": { initial: { opacity: 0, rotateY: 80 }, animate: { opacity: 1, rotateY: 0 } },
    "rotate-in": { initial: { opacity: 0, rotate: -8, scale: 0.95 }, animate: { opacity: 1, rotate: 0, scale: 1 } }
  };
  var BEZIER = /^cubic-bezier\(\s*(-?\d*\.?\d+)\s*,\s*(-?\d*\.?\d+)\s*,\s*(-?\d*\.?\d+)\s*,\s*(-?\d*\.?\d+)\s*\)$/;
  var presets = {};
  var key;
  for (key in BUILTIN) presets[key] = BUILTIN[key];
  for (key in C.presets || {}) presets[key] = C.presets[key];

  var reduceQuery = window.matchMedia ? window.matchMedia("(prefers-reduced-motion: reduce)") : null;
  function reduced() {
    if (C.reducedMotion === "always") return true;
    if (C.reducedMotion === "never") return false;
    return !!(reduceQuery && reduceQuery.matches);
  }

  function copy(src) {
    var out = {};
    for (var k in src) out[k] = src[k];
    return out;
  }

  function ease(value) {
    if (value === undefined || value === null || typeof value !== "string") return value;
    if (C.easings && C.easings[value]) return C.easings[value].slice();
    var m = BEZIER.exec(value);
    if (m) return [Number(m[1]), Number(m[2]), Number(m[3]), Number(m[4])];
    return value;
  }

  function transition(t) {
    var out = copy(C.transition || {});
    var k;
    if (t) for (k in t) out[k] = t[k];
    if (out.ease !== undefined) out.ease = ease(out.ease);
    return out;
  }

  function json(el, name) {
    var raw = el.getAttribute(name);
    if (raw === null || raw === "") return undefined;
    try {
      return JSON.parse(raw);
    } catch (e) {
      if (window.console) console.warn("[krate] motion: invalid JSON in " + name, raw);
      return undefined;
    }
  }

  function num(el, name) {
    var raw = el.getAttribute(name);
    if (raw === null || raw === "") return undefined;
    var n = Number(raw);
    return isNaN(n) ? undefined : n;
  }

  function flag(el, name, fallback) {
    if (!el.hasAttribute(name)) return fallback;
    var raw = el.getAttribute(name);
    return raw !== "false" && raw !== "0";
  }

  function last(v) {
    return Array.isArray(v) ? v[v.length - 1] : v;
  }

  function merge(a, b) {
    var out = copy(a || {});
    for (var k in b || {}) out[k] = b[k];
    return out;
  }

  function filterReduced(target) {
    if (!target || !reduced()) return target;
    var out = {};
    for (var k in target) if (!TRANSFORM_KEY.test(k)) out[k] = target[k];
    return out;
  }

  function resolve(el) {
    var name = el.getAttribute("data-motion");
    var preset = name && presets[name] ? presets[name] : null;
    if (name && !preset && window.console) console.warn("[krate] motion: unknown preset \"" + name + "\"");
    var spec = {
      initial: merge(preset && preset.initial, json(el, "data-motion-initial")),
      animate: merge(preset && preset.animate, json(el, "data-motion-animate")),
      hover: merge(preset && preset.hover, json(el, "data-motion-hover")),
      press: merge(preset && preset.press, json(el, "data-motion-press")),
      transition: merge(preset && preset.transition, json(el, "data-motion-transition")),
      scrollAnimate: json(el, "data-motion-scroll-animate"),
      scrollOffset: json(el, "data-motion-scroll-offset"),
      scroll: el.getAttribute("data-motion-scroll"),
      trigger: el.getAttribute("data-motion-trigger") || "view",
      viewport: {
        amount: el.hasAttribute("data-motion-amount") ? (isNaN(Number(el.getAttribute("data-motion-amount"))) ? el.getAttribute("data-motion-amount") : Number(el.getAttribute("data-motion-amount"))) : (C.viewport && C.viewport.amount !== undefined ? C.viewport.amount : "some"),
        margin: el.getAttribute("data-motion-margin") || (C.viewport && C.viewport.margin) || "0px",
        once: flag(el, "data-motion-once", C.viewport && C.viewport.once !== undefined ? C.viewport.once : true)
      }
    };
    var d = num(el, "data-motion-delay");
    var dur = num(el, "data-motion-duration");
    var e = el.getAttribute("data-motion-ease");
    if (d !== undefined) spec.transition.delay = d;
    if (dur !== undefined) spec.transition.duration = dur;
    if (e) spec.transition.ease = e;
    return spec;
  }

  function restFor(el, keys, spec) {
    var out = {};
    var computed = null;
    keys.forEach(function (k) {
      if (spec.animate && spec.animate[k] !== undefined) out[k] = last(spec.animate[k]);
      else if (spec.initial && spec.initial[k] !== undefined && spec.trigger === "none") out[k] = last(spec.initial[k]);
      else if (REST[k] !== undefined) out[k] = REST[k];
      else {
        computed = computed || getComputedStyle(el);
        out[k] = computed.getPropertyValue(k.replace(/[A-Z]/g, function (c) { return "-" + c.toLowerCase(); })) || computed[k];
      }
    });
    return out;
  }

  function play(el, target, t) {
    target = filterReduced(target);
    if (!target || !Object.keys(target).length) return null;
    return M.animate(el, target, transition(t));
  }

  function bindGestures(el, spec, cleanups) {
    if (Object.keys(spec.hover).length && M.hover) {
      var rest = restFor(el, Object.keys(spec.hover), spec);
      cleanups.push(M.hover(el, function () {
        play(el, spec.hover, spec.transition);
        return function () { play(el, rest, spec.transition); };
      }));
    }
    if (Object.keys(spec.press).length && M.press) {
      var pressRest = restFor(el, Object.keys(spec.press), spec);
      cleanups.push(M.press(el, function () {
        play(el, spec.press, spec.transition);
        return function () { play(el, pressRest, spec.transition); };
      }));
    }
  }

  function bindScroll(el, spec, cleanups) {
    if (!spec.scrollAnimate || !M.scroll) return;
    var target = filterReduced(spec.scrollAnimate);
    if (!target || !Object.keys(target).length) return;
    var animation = M.animate(el, target, { ease: "linear", duration: 1 });
    var opts = {};
    if (spec.scroll !== "page") opts.target = el;
    if (spec.scrollOffset) opts.offset = spec.scrollOffset;
    cleanups.push(M.scroll(animation, opts));
  }

  var cleanupMap = typeof WeakMap === "function" ? new WeakMap() : null;

  function ready(el) {
    el.setAttribute(READY, "");
  }

  function bind(el, extraDelay) {
    if (el.hasAttribute(BOUND)) return;
    el.setAttribute(BOUND, "");
    var spec = resolve(el);
    var cleanups = [];
    if (cleanupMap) cleanupMap.set(el, cleanups);
    if (extraDelay) spec.transition.delay = (spec.transition.delay || 0) + extraDelay;
    var hasEntrance = Object.keys(spec.animate).length > 0;
    if (Object.keys(spec.initial).length) {
      var initial = filterReduced(spec.initial);
      if (initial && Object.keys(initial).length) M.animate(el, initial, { duration: 0 });
    }
    ready(el);
    if (hasEntrance && spec.trigger !== "none") {
      if (spec.trigger === "load" || !M.inView) {
        play(el, spec.animate, spec.transition);
      } else {
        cleanups.push(M.inView(el, function () {
          play(el, spec.animate, spec.transition);
          if (spec.viewport.once) return undefined;
          return function () {
            if (Object.keys(spec.initial).length) play(el, spec.initial, spec.transition);
          };
        }, { amount: spec.viewport.amount, margin: spec.viewport.margin }));
      }
    }
    bindGestures(el, spec, cleanups);
    bindScroll(el, spec, cleanups);
  }

  function staggerFn(container) {
    var raw = container.getAttribute("data-motion-stagger");
    var s = C.stagger || {};
    var delay = raw !== null && raw !== "" && !isNaN(Number(raw)) ? Number(raw) : (s.delay !== undefined ? s.delay : 0.08);
    var from = container.getAttribute("data-motion-stagger-from");
    if (from === null) from = s.from !== undefined ? s.from : "first";
    else if (!isNaN(Number(from))) from = Number(from);
    var opts = { from: from };
    var e = container.getAttribute("data-motion-stagger-ease") || s.ease;
    if (e) opts.ease = ease(e);
    return M.stagger(delay, opts);
  }

  function refresh(scope) {
    var host = scope || doc;
    var all = Array.prototype.slice.call(host.querySelectorAll(SELECTOR));
    if (host.nodeType === 1 && host.matches && host.matches(SELECTOR)) all.unshift(host);
    var containers = Array.prototype.slice.call(host.querySelectorAll("[data-motion-stagger]"));
    var offsets = new Map();
    containers.forEach(function (container) {
      var items = all.filter(function (el) {
        return el !== container && container.contains(el) && !el.hasAttribute(BOUND);
      });
      if (!items.length) return;
      var fn = staggerFn(container);
      items.forEach(function (el, i) {
        if (!offsets.has(el)) offsets.set(el, fn(i, items.length));
      });
    });
    all.forEach(function (el) {
      bind(el, offsets.get(el) || 0);
    });
  }

  function run() {
    if (C.declarative !== false) refresh(doc);
    root.classList.remove("krate-motion-pending");
    root.classList.add("krate-motion-ready");
  }

  window.krateMotion = {
    config: C,
    presets: presets,
    refresh: refresh,
    resolve: resolve,
    ease: ease,
    transition: transition,
    reducedMotion: reduced
  };

  if (doc.readyState === "loading") doc.addEventListener("DOMContentLoaded", run);
  else run();
  window.addEventListener("krate:navigate-end", function () {
    if (C.declarative !== false) refresh(doc);
  });
})();
