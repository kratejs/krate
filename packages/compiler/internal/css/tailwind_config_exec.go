package css

// execConfigJS normalizes an evaluated tailwind.config into the JSON shape of
// TailwindConfig (theme overrides, theme.extend, dark mode, prefix, warnings).
const execConfigJS = `
function __krateNormalizeConfig(mod) {
  var c = (mod && mod.default) ? mod.default : mod;
  c = c || {};
  var warnings = [];
  function S(v) { return v == null ? "" : String(v); }
  function isObj(v) { return v !== null && typeof v === "object" && !Array.isArray(v); }
  function kebab(s) { return s.replace(/[A-Z]/g, function (m) { return "-" + m.toLowerCase(); }); }
  function flat(o, conv, path) {
    var r = {};
    if (!isObj(o)) return r;
    Object.keys(o).forEach(function (k) {
      var v = o[k];
      var x = conv ? conv(v) : (isObj(v) || Array.isArray(v) || typeof v === "function" ? null : S(v));
      if (x == null) { warnings.push("tailwind.config theme." + path + "." + k + ": unsupported value shape, ignored"); return; }
      r[k] = x;
    });
    return r;
  }
  function fam(v) {
    if (Array.isArray(v)) return v.map(function (x) { x = S(x); return /\s/.test(x) && !/^["']/.test(x) ? '"' + x + '"' : x; }).join(", ");
    return (isObj(v) || typeof v === "function") ? null : S(v);
  }
  function fsz(v) {
    if (Array.isArray(v)) {
      var o = "font-size: " + S(v[0]) + ";", e = v[1];
      if (isObj(e)) Object.keys(e).forEach(function (k) { o += " " + kebab(k) + ": " + S(e[k]) + ";"; });
      else if (e != null) o += " line-height: " + S(e) + ";";
      return o;
    }
    return (isObj(v) || typeof v === "function") ? null : S(v);
  }
  function scr(v) { return (typeof v === "string" || typeof v === "number") ? S(v) : null; }
  function col(o) {
    var r = {};
    if (!isObj(o)) return r;
    Object.keys(o).forEach(function (k) {
      var v = o[k];
      if (isObj(v)) {
        var m = {};
        Object.keys(v).forEach(function (s) { if (typeof v[s] === "string" || typeof v[s] === "number") m[s] = S(v[s]); });
        r[k] = m;
      } else if (typeof v === "string") r[k] = { DEFAULT: v };
    });
    return r;
  }
  function kfs(o) {
    var r = {};
    if (!isObj(o)) return r;
    Object.keys(o).forEach(function (n) {
      var b = "", fr = o[n] || {};
      Object.keys(fr).forEach(function (f) {
        b += f + "{";
        var p = fr[f] || {};
        Object.keys(p).forEach(function (k) { b += kebab(k) + ":" + S(p[k]) + ";"; });
        b += "}";
      });
      r[n] = b;
    });
    return r;
  }
  function norm(t) {
    t = t || {};
    var o = {};
    if (t.spacing) o.spacing = flat(t.spacing, null, "spacing");
    if (t.screens) o.screens = flat(t.screens, scr, "screens");
    if (t.maxWidth) o.maxWidth = flat(t.maxWidth, null, "maxWidth");
    if (t.minWidth) o.minWidth = flat(t.minWidth, null, "minWidth");
    if (t.lineHeight) o.lineHeight = flat(t.lineHeight, null, "lineHeight");
    if (t.opacity) o.opacity = flat(t.opacity, null, "opacity");
    if (t.fontFamily) o.fontFamily = flat(t.fontFamily, fam, "fontFamily");
    if (t.borderRadius) o.radii = flat(t.borderRadius, null, "borderRadius");
    if (t.boxShadow) o.shadows = flat(t.boxShadow, null, "boxShadow");
    if (t.fontSize) o.textSizes = flat(t.fontSize, fsz, "fontSize");
    if (t.fontWeight) o.fontWeights = flat(t.fontWeight, null, "fontWeight");
    if (t.letterSpacing) o.tracking = flat(t.letterSpacing, null, "letterSpacing");
    if (t.animation) o.animation = flat(t.animation, null, "animation");
    if (t.keyframes) o.keyframes = kfs(t.keyframes);
    if (t.colors) o.colors = col(t.colors);
    return o;
  }
  var th = c.theme;
  if (typeof th === "function") {
    try { th = th({ theme: function () { return {}; }, colors: {}, breakpoints: function (x) { return x; } }); }
    catch (e) { warnings.push("tailwind.config theme function failed: " + e); th = {}; }
  }
  th = th || {};
  var ext = th.extend || {};
  var theme = norm(th);
  var strat = "", sel = "", dm = c.darkMode;
  if (Array.isArray(dm)) {
    strat = S(dm[0]); sel = S(dm[1]);
    if (strat === "variant") { warnings.push("tailwind.config darkMode: the 'variant' form is not supported; use ['selector', '.dark']"); strat = "class"; sel = ""; }
  } else if (typeof dm === "string") strat = dm;
  theme.darkSelector = sel;
  if (c.prefix) {
    var p = S(c.prefix).replace(/[-:]+$/, "");
    theme.prefix = p;
    warnings.push("tailwind.config prefix '" + c.prefix + "': Krate uses the v4 prefix syntax (" + p + ":flex)");
  }
  if (c.important === true) theme.important = "true";
  else if (typeof c.important === "string") theme.important = c.important;
  if (Array.isArray(c.plugins) && c.plugins.length) warnings.push("tailwind.config plugins: JS plugins are not executed by Krate; utilities and variants they add will not be generated");
  ["presets", "corePlugins", "safelist", "blocklist", "separator"].forEach(function (k) {
    var v = c[k];
    if (v != null && !(Array.isArray(v) && !v.length) && !(isObj(v) && !Object.keys(v).length)) warnings.push("tailwind.config " + k + " is not supported by Krate and was ignored");
  });
  return { theme: theme, extend: norm(ext), darkMode: strat, warnings: warnings };
}
`
