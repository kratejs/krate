package build

// scriptcServerSource is the part of the generated scriptc API entry that is
// the same for every app: a node:http server that speaks the protocol of the
// Node API sidecar for the routes the host forwards to it. The routes table
// above it is generated. The source avoids default imports and exports, eval
// and dynamic import, which scriptc does not compile, and template literals so
// that it can live in a Go raw string.
//
// The host resolves the route file and sends it in the x-krate-route header,
// so the server holds no router of its own; it derives the dynamic params
// from the request path and the route's param spec.
const scriptcServerSource = `
const SECRET: string = process.env.KRATE_INTERNAL_SECRET ?? '';
const START: string = process.env.KRATE_SIDECAR_START ?? '';
const PORT: number = Number(process.env.KRATE_API_PORT ?? '3001');
const PARENT_PID: number = Number(process.env.KRATE_PARENT_PID ?? '0');

function sameSecret(a: string, b: string): boolean {
  if (a.length !== b.length) return false;
  let d = 0;
  for (let i = 0; i < a.length; i++) d |= a.charCodeAt(i) ^ b.charCodeAt(i);
  return d === 0;
}

function safeDecode(s: string): string {
  try {
    return decodeURIComponent(s);
  } catch (e) {
    return s;
  }
}

function paramsFor(spec: PSpec[], pathname: string): Record<string, string> {
  const out: Record<string, string> = {};
  if (spec.length === 0) return out;
  let rel = pathname;
  if (rel.startsWith('/api')) rel = rel.slice(4);
  const segs: string[] = [];
  for (const s of rel.split('/')) {
    if (s !== '') segs.push(safeDecode(s));
  }
  for (const p of spec) {
    if (p.kind === 0) {
      if (p.index < segs.length) out[p.name] = segs[p.index];
    } else if (p.kind === 1) {
      out[p.name] = segs.slice(p.index).join('/');
    } else if (p.index < segs.length) {
      out[p.name] = segs.slice(p.index).join('/');
    }
  }
  return out;
}

function sendJSON(res: ServerResponse, status: number, body: string): void {
  res.statusCode = status;
  res.setHeader('Content-Type', 'application/json');
  res.end(body);
}

async function handle(req: IncomingMessage, res: ServerResponse, body: Buffer): Promise<void> {
  const url = new URL(req.url ?? '/', 'http://' + (req.headers.host ?? 'localhost'));
  const file = req.headers['x-krate-route'];
  const route = typeof file === 'string' ? routes.get(file) : undefined;
  if (route === undefined) {
    sendJSON(res, 404, JSON.stringify({ error: 'API Route Not Found: ' + url.pathname }));
    return;
  }
  const method = (req.method ?? 'GET').toUpperCase();
  let fn = route.handlers.get(method);
  let headOnly = false;
  if (fn === undefined && method === 'HEAD') {
    fn = route.handlers.get('GET');
    headOnly = true;
  }
  if (fn === undefined) {
    res.setHeader('Allow', route.allow);
    if (method === 'OPTIONS') {
      res.statusCode = 204;
      res.end();
      return;
    }
    sendJSON(res, 405, '{"error":"Method Not Allowed"}');
    return;
  }
  const headers = new Headers();
  const raw = req.rawHeaders;
  for (let i = 0; i + 1 < raw.length; i += 2) {
    const name = raw[i].toLowerCase();
    if (name !== 'x-krate-internal' && name !== 'x-krate-route') headers.append(raw[i], raw[i + 1]);
  }
  // request.signal aborts when the client goes away before the response ends.
  const ac = new AbortController();
  res.on('close', () => {
    if (!res.writableEnded) ac.abort();
  });
  const init: RequestInit = { method, headers, signal: ac.signal };
  if (body.length > 0 && method !== 'GET' && method !== 'HEAD') init.body = body;
  const response = await fn(new Request(url.href, init), { params: paramsFor(route.params, url.pathname) });
  res.statusCode = response.status;
  response.headers.forEach((value: string, key: string) => {
    if (key !== 'set-cookie') res.setHeader(key, value);
  });
  const cookies = response.headers.getSetCookie();
  if (cookies.length > 0) res.setHeader('set-cookie', cookies);
  const stream = response.body;
  if (stream === null || headOnly || response.status === 204 || response.status === 304) {
    if (stream !== null) await stream.cancel();
    res.end();
    return;
  }
  const reader = stream.getReader();
  let r = await reader.read();
  if (!r.done) {
    // A body that is already complete ends in microtasks: send it with one
    // write so it gets a Content-Length. One still being produced is flushed
    // on the next macrotask so streaming stays prompt.
    const first = r.value;
    const pending = reader.read();
    const tick = new Promise<null>((ok) => {
      setImmediate(() => ok(null));
    });
    const settled = await Promise.race([pending, tick]);
    if (settled !== null && settled.done) {
      res.end(first);
      return;
    }
    res.write(first);
    r = settled !== null ? settled : await pending;
  }
  while (!r.done) {
    res.write(r.value);
    r = await reader.read();
  }
  res.end();
}

const server = createServer((req: IncomingMessage, res: ServerResponse) => {
  if (req.url === '/__krate/internal/ready') {
    res.setHeader('Content-Type', 'text/plain; charset=utf-8');
    res.end(START);
    return;
  }
  const secret = req.headers['x-krate-internal'];
  if (SECRET !== '' && (typeof secret !== 'string' || !sameSecret(secret, SECRET))) {
    res.statusCode = 403;
    res.setHeader('Content-Type', 'text/plain; charset=utf-8');
    res.end('forbidden');
    return;
  }
  const chunks: Buffer[] = [];
  req.on('data', (c: Buffer) => {
    chunks.push(c);
  });
  req.on('end', () => {
    handle(req, res, Buffer.concat(chunks)).catch((e: unknown) => {
      console.error('[krate] API error (' + (req.method ?? '') + ' ' + (req.url ?? '') + '): ' + String(e));
      if (res.headersSent) {
        res.destroy();
        return;
      }
      sendJSON(res, 500, '{"error":"Internal Server Error"}');
    });
  });
});

server.on('error', (e: Error) => {
  console.error('[krate] API server error: ' + String(e));
  process.exit(1);
});

// Exit when the Krate host dies: a SIGKILL gives it no chance to stop us.
if (PARENT_PID > 0) {
  setInterval(() => {
    try {
      process.kill(PARENT_PID, 0);
    } catch (e) {
      process.exit(0);
    }
  }, 2000);
}

server.listen(PORT, '127.0.0.1');
`
