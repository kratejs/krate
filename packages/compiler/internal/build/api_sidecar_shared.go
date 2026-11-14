package build

import "github.com/kratejs/krate/packages/native/jsruntime"

// apiSidecarSharedJS holds the API request semantics shared by the Node and
// Bun sidecars: method dispatch (405/OPTIONS/HEAD), error bodies, the
// Next-style cookies accessor, the middleware matcher and the middleware result
// envelope. It must not contain backticks (it is embedded in Go raw strings).
var apiSidecarSharedJS = jsruntime.MatcherJS + `
const KRATE_DEV = process.env.KRATE_API_DEV === '1';
const KRATE_METHODS = ['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'];
function krateAllow(mod) {
  const out = KRATE_METHODS.filter((m) => typeof mod[m] === 'function');
  if (out.includes('GET') && !out.includes('HEAD')) out.push('HEAD');
  if (out.length && !out.includes('OPTIONS')) out.push('OPTIONS');
  return out;
}
// krateHandler picks the handler for a method: a named export, GET for HEAD,
// the default export, else a 405 (Allow list) / automatic OPTIONS answer.
function krateHandler(mod, method) {
  let fn = mod[method];
  let named = typeof fn === 'function';
  let headOnly = false;
  if (!named && method === 'HEAD' && typeof mod.GET === 'function') { fn = mod.GET; named = true; headOnly = true; }
  if (named) return { fn, named, headOnly };
  if (typeof mod.default === 'function') return { fn: mod.default, named: false, headOnly: false };
  const allow = krateAllow(mod);
  if (method === 'OPTIONS' && allow.length) return { status: 204, allow };
  if (allow.length) return { status: 405, allow };
  return { status: 500, error: 'API route must export a handler function (GET, POST, etc. or default).' };
}
// Request budget: KRATE_API_TIMEOUT_MS unless the route's maxDuration (read by
// the build into api-routes.json) says otherwise. A handler that has not
// returned its Response by then is aborted (request.signal) and answered 504.
const KRATE_API_TIMEOUT_MS = parseInt(process.env.KRATE_API_TIMEOUT_MS || '', 10) > 0 ? parseInt(process.env.KRATE_API_TIMEOUT_MS, 10) : 30000;
let krateBudgets = null;
let krateBudgetStamp = '';
function krateRouteBudgetMs(apiDir, file) {
  const metaPath = process.env.KRATE_API_ROUTE_META;
  if (!metaPath) return KRATE_API_TIMEOUT_MS;
  if (!krateBudgets || HOT) {
    try {
      const st = fs.statSync(metaPath);
      const stamp = st.mtimeMs + ':' + st.size;
      if (stamp !== krateBudgetStamp) {
        const next = new Map();
        for (const m of JSON.parse(fs.readFileSync(metaPath, 'utf8'))) {
          if (m && typeof m.file === 'string' && m.maxDuration > 0) next.set(m.file, m.maxDuration * 1000);
        }
        krateBudgets = next;
        krateBudgetStamp = stamp;
      }
    } catch {
      if (!krateBudgets) krateBudgets = new Map();
    }
  }
  const rel = path.relative(apiDir, file).split(path.sep).join('/');
  return krateBudgets.get(rel.replace(/\.[^./]+$/, '.js')) || KRATE_API_TIMEOUT_MS;
}
function krateWithBudget(ms, ac, run) {
  let timer;
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => {
      const err = new Error('API route exceeded its request budget of ' + ms / 1000 + 's (maxDuration or KRATE_API_TIMEOUT)');
      err.krateTimeout = true;
      reject(err);
      try { ac.abort(err); } catch {}
    }, ms);
  });
  return Promise.race([run(), timeout]).finally(() => clearTimeout(timer));
}
function krateErrorBody(err, where) {
  console.error('[krate] API error (' + where + '): ' + ((err && err.stack) || err));
  const msg = (err && err.message) || String(err);
  return KRATE_DEV ? { error: 'Internal Server Error', details: msg } : { error: 'Internal Server Error' };
}
function krateCookies(headers) {
  const jar = new Map();
  for (const part of String(headers.get('cookie') || '').split(';')) {
    const i = part.indexOf('=');
    if (i <= 0) continue;
    const name = part.slice(0, i).trim();
    if (name && !jar.has(name)) jar.set(name, part.slice(i + 1).trim());
  }
  return {
    get: (n) => (jar.has(String(n)) ? { name: String(n), value: jar.get(String(n)) } : undefined),
    getAll: (n) => [...jar].filter(([k]) => n === undefined || k === String(n)).map(([name, value]) => ({ name, value })),
    has: (n) => jar.has(String(n)),
  };
}
// krateNextUrl is request.nextUrl: the request URL with NextURL's basePath and
// clone(). The host strips the base path before forwarding, so pathname never
// includes it, as in Next.
function krateNextUrl(href) {
  const u = new URL(href);
  Object.defineProperty(u, 'basePath', { value: process.env.KRATE_BASE_PATH || '', enumerable: false });
  Object.defineProperty(u, 'clone', { value: () => krateNextUrl(u.href), enumerable: false });
  return u;
}
function krateAttach(request) {
  try { Object.defineProperty(request, 'cookies', { value: krateCookies(request.headers), enumerable: false }); } catch {}
  try { Object.defineProperty(request, 'nextUrl', { value: krateNextUrl(request.url), enumerable: false }); } catch {}
  return request;
}
function krateHeaderPairs(headers) {
  const pairs = [];
  const cookies = typeof headers.getSetCookie === 'function' ? headers.getSetCookie() : [];
  for (const [k, v] of headers) { if (k.toLowerCase() !== 'set-cookie') pairs.push([k, v]); }
  for (const c of cookies) pairs.push(['set-cookie', c]);
  return pairs;
}
function krateRequestHeaders(map) {
  const h = new Headers();
  for (const [k, v] of Object.entries(map || {})) {
    if (Array.isArray(v)) { for (const x of v) h.append(k, String(x)); }
    else if (typeof v === 'string') h.append(k, v);
  }
  return h;
}
// krateMiddlewareFn picks the handler the way the embedded runtime does: the
// named export, then the default one. A module with neither has no handler, which
// the caller must answer with an error: skipping it would skip an auth check.
function krateMiddlewareFn(mod) {
  if (typeof mod.middleware === 'function') return mod.middleware;
  return typeof mod.default === 'function' ? mod.default : null;
}
const krateNoMiddlewareFn = 'middleware.js does not export a middleware function';
// krateLoadMiddleware imports the middleware module, and in dev (HOT)
// re-imports it whenever the file changes so edits apply without restarting the
// sidecar. A failed reload keeps the previous module and logs the error.
let krateMwStamp = '';
// krateMwError holds the load error of a module that exists but failed to
// import. With no previously loaded module the host must fail closed instead
// of treating the app as having no middleware.
let krateMwError = '';
// krateImportFresh imports a module with a cache-busting tag. Bun ignores the
// query of a file:// URL (it would serve the first copy forever), but honors it
// on a plain absolute path.
function krateImportFresh(file, tag) {
  const q = '?t=' + encodeURIComponent(tag);
  return import(typeof Bun !== 'undefined' ? file + q : pathToFileURL(file).href + q);
}
async function krateLoadMiddleware() {
  if (!MIDDLEWARE_PATH) return;
  let stamp = '';
  try {
    const st = fs.statSync(MIDDLEWARE_PATH);
    if (!st.isFile()) return;
    stamp = st.mtimeMs + ':' + st.size;
  } catch {
    if (HOT) { middlewareModule = null; krateMwStamp = ''; krateMwError = ''; }
    return;
  }
  if (stamp === krateMwStamp) return;
  krateMwStamp = stamp;
  try {
    middlewareModule = await krateImportFresh(MIDDLEWARE_PATH, stamp);
    krateMwError = '';
  } catch (e) {
    krateMwError = (e && e.message) || String(e);
    console.error('[krate] middleware load error: ' + ((e && e.stack) || e));
  }
}
// krateMiddlewareJSON converts a middleware return value into the envelope the
// Go host understands: continue (passthrough / rewrite) or a response.
async function krateMiddlewareJSON(result) {
  if (result === undefined || result === null) return { action: 'continue', headers: [] };
  if (typeof result.status !== 'number') return { action: 'continue', headers: [] };
  let next = false;
  let rewrite = '';
  const headers = [];
  for (const [k, v] of krateHeaderPairs(result.headers)) {
    if (k === 'x-middleware-next') next = true;
    else if (k === 'x-middleware-rewrite') rewrite = v;
    else headers.push([k, v]);
  }
  if (next || rewrite) return { action: 'continue', headers, rewrite };
  return { action: 'response', status: result.status, headers, body: result.status === 204 ? '' : await result.text() };
}

// krateMatchers compiles the middleware module's matcher once (undefined means
// no matcher: the middleware runs for every request the host forwards).
let krateMwMatchers;
function krateMatchersFor(mod) {
  if (krateMwMatchers !== undefined) return krateMwMatchers;
  const m = mod.matcher !== undefined ? mod.matcher : mod.config && mod.config.matcher;
  if (m === undefined || m === null) return (krateMwMatchers = null);
  krateMwMatchers = (Array.isArray(m) ? m : [m]).map((e) => {
    if (e && typeof e === 'object') {
      if (e.has || e.missing || e.locale === false) throw new Error('middleware matcher has/missing/locale conditions are not supported; use a path-only matcher');
      return __krate_compile_matcher(e.source);
    }
    return __krate_compile_matcher(e);
  });
  return krateMwMatchers;
}
function krateMiddlewareMatches(mod, path) {
  const ms = krateMatchersFor(mod);
  return !ms || ms.some((re) => re.test(path));
}
`
