// Seals the arguments a server action was bound to with .bind() when the action
// crosses into a client component, in the format of the closure tokens of inline
// server actions (compiler/internal/build/inline_actions.go): AES-256-GCM, the
// action id as associated data, a "k1." prefix. The server action endpoint opens
// the token and puts the values in front of the client's own arguments, so the
// browser never sees or sends them.

const MAX_URL_TOKEN = 3500;

type Builtin = (name: string) => any;

function builtin(name: string): any {
  const get = (globalThis as { process?: { getBuiltinModule?: Builtin } }).process?.getBuiltinModule;
  if (typeof get !== 'function') {
    throw new Error('sealing bound server action arguments needs Node.js (process.getBuiltinModule)');
  }
  return get(name);
}

function actionSecret(): string {
  const proc = (globalThis as { process?: { env?: Record<string, string | undefined>; cwd?: () => string } }).process;
  let secret = proc?.env?.KRATE_ACTION_SECRET || '';
  if (!secret && !(globalThis as Record<symbol, unknown>)[Symbol.for('krate.action.deployed')]) {
    const fs = builtin('node:fs');
    const path = builtin('node:path');
    const dir = proc?.env?.KRATE_ACTION_DIR || path.join(proc?.env?.KRATE_ROOT || proc?.cwd?.() || '.', '.krate', 'actions');
    try {
      secret = JSON.parse(fs.readFileSync(path.join(dir, 'manifest.json'), 'utf8')).key || '';
    } catch {
      secret = '';
    }
  }
  if (!secret) {
    throw new Error('bound server action arguments need an encryption key: set KRATE_ACTION_SECRET, or run the server from a build whose .krate/actions/manifest.json holds one');
  }
  return secret;
}

function describe(v: unknown): string {
  if (typeof v === 'function') return 'a function';
  if (typeof v === 'symbol') return 'a symbol';
  if (typeof v === 'number') return 'the number ' + v;
  if (v && typeof (v as { then?: unknown }).then === 'function') return 'a Promise (await it before binding it)';
  const proto = v && Object.getPrototypeOf(v);
  return proto && proto.constructor && proto.constructor.name ? 'a ' + proto.constructor.name + ' instance' : 'a non-plain object';
}

function encode(v: unknown, where: string, seen: Set<unknown>): unknown {
  if (v === null || typeof v === 'string' || typeof v === 'boolean') return v;
  if (typeof v === 'number') {
    if (!Number.isFinite(v)) throw new TypeError(where + ', which is ' + describe(v));
    return v;
  }
  if (v === undefined) return { $: 'u' };
  if (typeof v === 'bigint') return { $: 'n', v: v.toString() };
  if (v instanceof Date) return { $: 'd', v: v.toISOString() };
  if (typeof v === 'function' || typeof v === 'symbol') throw new TypeError(where + ', which is ' + describe(v));
  if (seen.has(v)) throw new TypeError(where + ', which is circular');
  seen.add(v);
  try {
    if (Array.isArray(v)) return v.map((x, i) => encode(x, where + '[' + i + ']', seen));
    const proto = Object.getPrototypeOf(v);
    if (proto !== Object.prototype && proto !== null) throw new TypeError(where + ', which is ' + describe(v));
    const out: Record<string, unknown> = {};
    for (const k of Object.keys(v as object)) out[k] = encode((v as Record<string, unknown>)[k], where + '.' + k, seen);
    return Object.prototype.hasOwnProperty.call(out, '$') ? { $: 'o', v: out } : out;
  } finally {
    seen.delete(v);
  }
}

/**
 * Returns the sealed token for the values `action.bind(null, ...values)` fixed.
 * `path` names the prop for error messages (`props.action`).
 */
export function sealBoundArgs(id: string, values: unknown[], path: string): string {
  const encoded = values.map((v, i) => {
    try {
      return encode(v, `argument ${i + 1}`, new Set());
    } catch (e) {
      throw new TypeError(
        `Prop \`${path}\` is a server action bound to a value that cannot be sent to a client component: ${(e as Error).message}. ` +
          'Only strings, numbers, booleans, null, undefined, bigint, Date, and plain objects and arrays of those can be bound; read anything else inside the action.',
      );
    }
  });
  const crypto = builtin('node:crypto');
  const key = crypto.createHash('sha256').update('krate-inline-action\0' + actionSecret()).digest();
  const iv = crypto.randomBytes(12);
  const cipher = crypto.createCipheriv('aes-256-gcm', key, iv);
  cipher.setAAD(Buffer.from(id));
  const body = Buffer.concat([cipher.update(JSON.stringify(encoded), 'utf8'), cipher.final()]);
  const token = 'k1.' + Buffer.concat([iv, body, cipher.getAuthTag()]).toString('base64url');
  if (token.length > MAX_URL_TOKEN) {
    throw new RangeError(`Prop \`${path}\` is a server action bound to ${token.length} bytes of data; bind ids and look the rest up inside the action`);
  }
  return token;
}
