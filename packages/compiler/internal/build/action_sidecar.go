package build

// actionSidecarJS is the server-action endpoint shared by the Node (node:http,
// also used for Deno) and Bun sidecars. It works on web Request/Response
// objects; each sidecar adapts its own request type. It expects path, fs,
// pathToFileURL, ACTION_DIR and HOT to be defined by the host script.
//
// Wire format (see guides/server-actions.md):
//
//	request   application/json  {"args":[...]}
//	          multipart/urlencoded form fields; "__krate_args" (field, or query
//	          string for no-JS posts) holds a JSON array of leading arguments
//	          and the FormData is passed last
//	response  fetch mode (x-krate-action: fetch):
//	            200 {"ok":true,"result":R,"redirect":"/path"?}
//	            4xx/5xx {"ok":false,"error":{"message":M,"digest":D?}}
//	          native form post: 303 to the action's redirect, the sanitized
//	          X-Krate-Return path, or "/"
//
// Actions run inside an AsyncLocalStorage scope published on
// globalThis[Symbol.for('krate.action.store')]; @krate/runtime/server reads it
// for headers(), cookies() and getActionContext().
//
// Actions and API routes also run inside a revalidation scope
// (globalThis[Symbol.for('krate.revalidate.store')]): revalidatePath and
// revalidateTag from @krate/runtime/cache queue on it, and the sidecar sends
// the queue to the Krate host (KRATE_HOST_URL, authenticated with
// KRATE_INTERNAL_SECRET) and waits for it to be applied before it responds.
// A fetch-mode action response then carries "revalidated":true and
// "revalidation":{paths,tags}. It expects timingSafeEqual to be imported.
const actionSidecarJS = `
const actionModuleCache = new Map();
let actionManifest = null;
let actionManifestTime = 0;
const ACTION_STORE = new AsyncLocalStorage();
globalThis[Symbol.for('krate.action.store')] = ACTION_STORE;
const REVALIDATE_STORE = new AsyncLocalStorage();
globalThis[Symbol.for('krate.revalidate.store')] = REVALIDATE_STORE;
const KRATE_HOST_URL = process.env.KRATE_HOST_URL || '';
const KRATE_INTERNAL_SECRET = process.env.KRATE_INTERNAL_SECRET || '';

// The data-cache runtime (Next-style fetch caching, shared cache state) staged
// by the host. Without it fetch cache options would be silently ignored, so a
// load failure is reported.
if (process.env.KRATE_CACHE_RUNTIME) {
  try {
    await import(pathToFileURL(process.env.KRATE_CACHE_RUNTIME).href);
  } catch (e) {
    console.error('[krate] data cache runtime failed to load; fetch cache options are not applied:', e);
  }
}

function newRevalidateScope(kind) {
  return { kind, paths: [], tags: [] };
}

// flushRevalidate applies a request's queued revalidations on the host and
// resolves with {paths, tags} (null when nothing was queued). It throws when
// the host cannot be reached or refuses, so the request fails loudly.
async function flushRevalidate(scope) {
  if (!scope || (scope.paths.length === 0 && scope.tags.length === 0)) return null;
  const payload = { paths: scope.paths.slice(), tags: Array.from(new Set(scope.tags)) };
  scope.paths.length = 0;
  scope.tags.length = 0;
  if (!KRATE_HOST_URL || !KRATE_INTERNAL_SECRET) {
    throw new Error('revalidatePath()/revalidateTag() need the Krate server: KRATE_HOST_URL is not set');
  }
  const res = await fetch(KRATE_HOST_URL + '/__krate/internal/revalidate', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-krate-internal': KRATE_INTERNAL_SECRET },
    body: JSON.stringify(payload),
  });
  const text = await res.text();
  if (!res.ok) throw new Error('revalidation failed: host answered ' + res.status + ' ' + text);
  let out = {};
  try { out = JSON.parse(text); } catch {}
  for (const s of out.skipped || []) console.warn('[krate] revalidation skipped for ' + s.target + ': ' + s.reason);
  return payload;
}

function internalAuthorized(value) {
  if (!KRATE_INTERNAL_SECRET || typeof value !== 'string') return false;
  const a = Buffer.from(value);
  const b = Buffer.from(KRATE_INTERNAL_SECRET);
  return a.length === b.length && timingSafeEqual(a, b);
}

// applyInternalLocal applies a host operation to this process's caches.
function applyInternalLocal(op, payload) {
  if (op === 'cache-invalidate') {
    const rt = globalThis[Symbol.for('krate.cache.runtime')];
    const tags = Array.isArray(payload && payload.tags) ? payload.tags.filter((t) => typeof t === 'string') : [];
    if (rt && typeof rt.invalidate === 'function') return rt.invalidate({ tags });
    return { dataEntries: 0 };
  }
  return { error: 'unknown internal op ' + op };
}

// broadcastInternal applies an operation on every worker; the Node script
// replaces it with a cluster fan-out when clustered.
let broadcastInternal = async (op, payload) => [applyInternalLocal(op, payload)];

async function internalCacheInvalidate(headerValue, bodyText) {
  if (!internalAuthorized(headerValue)) return { status: 403, body: { error: 'forbidden' } };
  let payload = {};
  try { payload = JSON.parse(bodyText || '{}'); } catch { return { status: 400, body: { error: 'invalid body' } }; }
  const results = await broadcastInternal('cache-invalidate', payload);
  const failed = results.filter((r) => r && r.error);
  if (failed.length) return { status: 500, body: { error: failed.map((r) => r.error).join('; ') } };
  return { status: 200, body: { ok: true, workers: results.length, dataEntries: results.reduce((n, r) => n + ((r && r.dataEntries) || 0), 0) } };
}

function loadActionManifest() {
  const manifestPath = path.join(ACTION_DIR, 'manifest.json');
  try {
    const st = fs.statSync(manifestPath);
    if (!actionManifest || st.mtimeMs !== actionManifestTime) {
      actionManifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
      actionManifestTime = st.mtimeMs;
    }
  } catch {
    actionManifest = { actions: [] };
  }
  return actionManifest;
}

function actionHeaders(type, cookies) {
  const headers = new Headers({ 'content-type': type, 'cache-control': 'no-store' });
  for (const c of cookies || []) headers.append('set-cookie', c);
  return headers;
}

function actionJSON(body, status, cookies) {
  return new Response(JSON.stringify(body), { status, headers: actionHeaders('application/json', cookies) });
}

function actionFail(status, message, digest, cookies) {
  const error = { message };
  if (digest) error.digest = digest;
  return actionJSON({ ok: false, error }, status, cookies);
}

function parseActionCookies(header) {
  const jar = new Map();
  for (const part of String(header || '').split(';')) {
    const i = part.indexOf('=');
    if (i <= 0) continue;
    const name = part.slice(0, i).trim();
    if (!name || jar.has(name)) continue;
    let value = part.slice(i + 1).trim();
    if (value.startsWith('"') && value.endsWith('"')) value = value.slice(1, -1);
    try { value = decodeURIComponent(value); } catch {}
    jar.set(name, value);
  }
  return jar;
}

const COOKIE_NAME = /^[!#$%&'*+\-.^_\x60|~0-9A-Za-z]+$/;

function serializeActionCookie(name, value, opts) {
  const o = opts || {};
  if (!COOKIE_NAME.test(name)) throw new TypeError('invalid cookie name: ' + name);
  let s = name + '=' + encodeURIComponent(String(value));
  const p = o.path === undefined ? '/' : String(o.path);
  if (p && !/[;\r\n]/.test(p)) s += '; Path=' + p;
  if (o.domain && !/[;\r\n]/.test(String(o.domain))) s += '; Domain=' + o.domain;
  if (typeof o.maxAge === 'number' && isFinite(o.maxAge)) s += '; Max-Age=' + Math.floor(o.maxAge);
  if (o.expires instanceof Date) s += '; Expires=' + o.expires.toUTCString();
  if (o.httpOnly !== false) s += '; HttpOnly';
  if (o.secure) s += '; Secure';
  const same = o.sameSite === undefined ? 'Lax' : o.sameSite;
  if (same) s += '; SameSite=' + (same === true ? 'Strict' : String(same).replace(/[^A-Za-z]/g, ''));
  return s;
}

function makeActionContext(req, id, urlPath) {
  const jar = parseActionCookies(req.headers.get('cookie'));
  const setCookies = [];
  const cookies = {
    get(name) { return jar.get(name); },
    getAll() { return Array.from(jar, ([name, value]) => ({ name, value })); },
    has(name) { return jar.has(name); },
    set(name, value, opts) { setCookies.push(serializeActionCookie(name, value, opts)); jar.set(name, String(value)); },
    delete(name, opts) { setCookies.push(serializeActionCookie(name, '', Object.assign({}, opts, { maxAge: 0 }))); jar.delete(name); },
  };
  return { actionId: id, url: urlPath, method: 'POST', headers: req.headers, cookies, setCookies };
}

function leadingActionArgs(raw) {
  if (raw == null || raw === '') return [];
  try {
    const v = JSON.parse(String(raw));
    return Array.isArray(v) ? v : [];
  } catch {
    return null;
  }
}

async function krateRunAction(req, urlPath, search) {
  let id = '';
  try { id = decodeURIComponent(urlPath.slice('/__krate/action/'.length)); } catch {}
  const entry = (loadActionManifest().actions || []).find((a) => a.id === id);
  if (!entry) return actionFail(404, 'Server action not found');
  const file = path.join(ACTION_DIR, entry.file || '');
  if (!file.startsWith(ACTION_DIR + path.sep)) return actionFail(404, 'Server action not found');
  let mod = actionModuleCache.get(file);
  if (!mod || HOT) {
    mod = await import(pathToFileURL(file).href + (HOT ? '?t=' + Date.now() : ''));
    if (!HOT) actionModuleCache.set(file, mod);
  }
  const fn = entry.export === 'default' ? (mod.default || mod[entry.export]) : mod[entry.export];
  if (typeof fn !== 'function') return actionFail(500, 'Server action export is not a function');

  const fetchMode = String(req.headers.get('x-krate-action') || '') === 'fetch';
  const query = new URLSearchParams(search || '');
  let args;
  try {
    if (String(req.headers.get('content-type') || '').includes('application/json')) {
      const payload = await req.json();
      args = payload && Array.isArray(payload.args) ? payload.args : [];
    } else {
      const fd = await req.formData();
      let raw = fd.get('__krate_args');
      fd.delete('__krate_args');
      if (raw == null) raw = query.get('__krate_args');
      args = leadingActionArgs(raw);
      if (args === null) throw new Error('bad __krate_args');
      args.push(fd);
    }
  } catch {
    return actionFail(400, 'Invalid server action request body');
  }

  const ctx = makeActionContext(req, id, urlPath);
  const revalidateScope = newRevalidateScope('action');
  let result;
  let redirect = '';
  let failure = null;
  try {
    result = await REVALIDATE_STORE.run(revalidateScope, () => ACTION_STORE.run(ctx, () => fn(...args)));
  } catch (e) {
    if (e && typeof e.__krateRedirect === 'string') redirect = e.__krateRedirect;
    else failure = e === undefined ? new Error('undefined') : e;
  }
  // Revalidations are applied before the response (also when the action
  // failed after calling them): the next render reads the new data.
  let revalidation = null;
  try {
    revalidation = await flushRevalidate(revalidateScope);
  } catch (e) {
    if (!failure) failure = e;
  }
  if (!redirect && result && typeof result === 'object' && typeof result.redirect === 'string') redirect = result.redirect;
  let body = '';
  if (!failure && fetchMode) {
    const envelope = { ok: true, result: result === undefined ? null : result };
    if (redirect) envelope.redirect = redirect;
    if (revalidation) {
      envelope.revalidated = true;
      envelope.revalidation = revalidation;
    }
    try { body = JSON.stringify(envelope); } catch (e) { failure = e; }
  }
  if (failure) {
    const digest = randomBytes(6).toString('hex');
    console.error('[krate] server action ' + entry.export + ' (' + (entry.source || id) + ') failed, digest ' + digest + ':', failure);
    const message = HOT ? String((failure && failure.message) || failure) : 'Server action failed';
    if (fetchMode) return actionFail(500, message, digest, ctx.setCookies);
    return new Response('Server action failed (digest ' + digest + ')\n', { status: 500, headers: actionHeaders('text/plain; charset=utf-8', ctx.setCookies) });
  }
  if (fetchMode) return new Response(body, { status: 200, headers: actionHeaders('application/json', ctx.setCookies) });
  const headers = actionHeaders('text/plain; charset=utf-8', ctx.setCookies);
  headers.set('location', redirect || req.headers.get('x-krate-return') || '/');
  return new Response(null, { status: 303, headers });
}
`

// actionSidecarNodeAdapter turns a node:http request into a web Request for
// krateRunAction and writes the Response back.
const actionSidecarNodeAdapter = `
async function sendActionResponse(res, response) {
  res.statusCode = response.status;
  response.headers.forEach((value, key) => { if (key !== 'set-cookie') res.setHeader(key, value); });
  const cookies = typeof response.headers.getSetCookie === 'function' ? response.headers.getSetCookie() : [];
  if (cookies.length) res.setHeader('set-cookie', cookies);
  res.end(Buffer.from(await response.arrayBuffer()));
}

async function runAction(req, res, urlObj) {
  const chunks = [];
  for await (const chunk of req) chunks.push(chunk);
  const headers = new Headers();
  for (const [k, v] of Object.entries(req.headers)) {
    if (v !== undefined) headers.set(k, Array.isArray(v) ? v.join(k === 'cookie' ? '; ' : ', ') : v);
  }
  const request = new Request('http://localhost' + urlObj.pathname + urlObj.search, { method: 'POST', headers, body: Buffer.concat(chunks) });
  return sendActionResponse(res, await krateRunAction(request, urlObj.pathname, urlObj.search));
}
`

// apiSidecarClusterJS fans host operations out to every clustered Node API
// worker through the primary, like the SSR renderer: the worker that received
// the request answers once all live workers acknowledged.
const apiSidecarClusterJS = `
if (WORKERS > 1 && cluster.isPrimary) {
  const broadcasts = new Map();
  const finish = (id) => {
    const b = broadcasts.get(id);
    if (!b || b.remaining.size > 0) return;
    broadcasts.delete(id);
    if (b.origin.isConnected()) b.origin.send({ type: 'krate:bcast-done', id, results: b.results });
  };
  cluster.on('message', (worker, msg) => {
    if (!msg || typeof msg !== 'object') return;
    if (msg.type === 'krate:bcast') {
      const live = Object.values(cluster.workers || {}).filter((w) => w && w.isConnected());
      broadcasts.set(msg.id, { origin: worker, remaining: new Set(live.map((w) => w.id)), results: [] });
      for (const w of live) w.send({ type: 'krate:apply', id: msg.id, op: msg.op, payload: msg.payload });
      finish(msg.id);
    } else if (msg.type === 'krate:applied') {
      const b = broadcasts.get(msg.id);
      if (!b) return;
      b.results.push(msg.result);
      b.remaining.delete(worker.id);
      finish(msg.id);
    }
  });
  cluster.on('exit', (worker) => {
    for (const [id, b] of broadcasts) if (b.remaining.delete(worker.id)) finish(id);
  });
} else if (cluster.isWorker) {
  let seq = 0;
  const waiters = new Map();
  process.on('message', (msg) => {
    if (!msg || typeof msg !== 'object') return;
    if (msg.type === 'krate:apply') {
      let result;
      try { result = applyInternalLocal(msg.op, msg.payload || {}); } catch (e) { result = { error: String((e && e.message) || e) }; }
      process.send({ type: 'krate:applied', id: msg.id, result });
    } else if (msg.type === 'krate:bcast-done') {
      const done = waiters.get(msg.id);
      if (done) { waiters.delete(msg.id); done(Array.isArray(msg.results) ? msg.results : []); }
    }
  });
  broadcastInternal = (op, payload) => new Promise((resolve) => {
    const id = process.pid + '-' + (++seq);
    waiters.set(id, resolve);
    process.send({ type: 'krate:bcast', id, op, payload });
  });
}
`
