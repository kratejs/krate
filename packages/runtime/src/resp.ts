// A small RESP2 client over node:net / node:tls, enough for the Redis cache
// adapter: pipelined commands with a timeout each, automatic reconnects, and a
// subscriber connection for pub/sub. No dependency.

import net from 'node:net';
import tls from 'node:tls';

export type RespValue = string | number | null | RespValue[] | Buffer;

export class RespError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'RespError';
  }
}

export interface RespOptions {
  host: string;
  port: number;
  tls?: boolean;
  username?: string;
  password?: string;
  db?: number;
  /** Milliseconds to wait for a connection (default 1000). */
  connectTimeoutMs?: number;
  /** Milliseconds to wait for one reply (default 1000). */
  commandTimeoutMs?: number;
}

/** Parses redis://[user:password@]host[:port][/db] and rediss:// (TLS) URLs. */
export function parseRedisURL(url: string): RespOptions {
  const u = new URL(url);
  if (u.protocol !== 'redis:' && u.protocol !== 'rediss:') throw new Error(`unsupported cache URL protocol ${u.protocol}`);
  const db = u.pathname.length > 1 ? parseInt(u.pathname.slice(1), 10) : undefined;
  return {
    host: u.hostname || '127.0.0.1',
    port: u.port ? parseInt(u.port, 10) : 6379,
    tls: u.protocol === 'rediss:',
    username: u.username ? decodeURIComponent(u.username) : undefined,
    password: u.password ? decodeURIComponent(u.password) : undefined,
    db: db !== undefined && Number.isFinite(db) ? db : undefined,
  };
}

/** Encodes a command as a RESP array of bulk strings. */
export function encodeCommand(args: Array<string | number | Buffer>): Buffer {
  const parts: Buffer[] = [Buffer.from(`*${args.length}\r\n`)];
  for (const a of args) {
    const b = Buffer.isBuffer(a) ? a : Buffer.from(String(a));
    parts.push(Buffer.from(`$${b.length}\r\n`), b, Buffer.from('\r\n'));
  }
  return Buffer.concat(parts);
}

/** Incremental reply parser: push chunks, take complete replies. */
export class RespParser {
  private buf: Buffer = Buffer.alloc(0);

  push(chunk: Buffer): void {
    this.buf = this.buf.length === 0 ? chunk : Buffer.concat([this.buf, chunk]);
  }

  /** The next complete reply, or undefined when more bytes are needed. */
  next(): RespValue | RespError | undefined {
    const r = this.parse(0);
    if (!r) return undefined;
    this.buf = this.buf.subarray(r.end);
    return r.value;
  }

  private line(at: number): { text: string; end: number } | null {
    const i = this.buf.indexOf('\r\n', at);
    if (i < 0) return null;
    return { text: this.buf.toString('utf8', at, i), end: i + 2 };
  }

  private parse(at: number): { value: RespValue | RespError; end: number } | null {
    if (at >= this.buf.length) return null;
    const type = String.fromCharCode(this.buf[at]);
    const l = this.line(at + 1);
    if (!l) return null;
    switch (type) {
      case '+':
        return { value: l.text, end: l.end };
      case '-':
        return { value: new RespError(l.text), end: l.end };
      case ':':
        return { value: parseInt(l.text, 10), end: l.end };
      case '$': {
        const n = parseInt(l.text, 10);
        if (n < 0) return { value: null, end: l.end };
        if (this.buf.length < l.end + n + 2) return null;
        return { value: this.buf.toString('utf8', l.end, l.end + n), end: l.end + n + 2 };
      }
      case '*': {
        const n = parseInt(l.text, 10);
        if (n < 0) return { value: null, end: l.end };
        const items: RespValue[] = [];
        let pos = l.end;
        for (let i = 0; i < n; i++) {
          const r = this.parse(pos);
          if (!r) return null;
          items.push(r.value instanceof RespError ? r.value.message : r.value);
          pos = r.end;
        }
        return { value: items, end: pos };
      }
      default:
        throw new RespError(`unexpected reply type ${JSON.stringify(type)}`);
    }
  }
}

interface Pending {
  resolve(v: RespValue): void;
  reject(e: Error): void;
  timer: ReturnType<typeof setTimeout>;
}

/** One connection: commands are written in order and answered in order. */
export class RespConnection {
  private socket: net.Socket | null = null;
  private connecting: Promise<net.Socket> | null = null;
  private pending: Pending[] = [];
  private parser = new RespParser();
  private closed = false;
  /** Called with each pushed pub/sub message (type, channel, payload). */
  onPush?: (kind: string, channel: string, payload: string) => void;
  /** Called after the socket dropped (not after close()). */
  onDisconnect?: (err?: Error) => void;

  constructor(private readonly opts: RespOptions) {}

  private connect(): Promise<net.Socket> {
    if (this.socket) return Promise.resolve(this.socket);
    if (this.connecting) return this.connecting;
    const timeout = this.opts.connectTimeoutMs ?? 1000;
    this.connecting = new Promise<net.Socket>((resolve, reject) => {
      const done = (err: Error | null, s?: net.Socket) => {
        clearTimeout(timer);
        this.connecting = null;
        if (err) reject(err);
        else resolve(s as net.Socket);
      };
      const s: net.Socket = this.opts.tls
        ? tls.connect({ host: this.opts.host, port: this.opts.port, servername: this.opts.host })
        : net.connect({ host: this.opts.host, port: this.opts.port });
      const timer = setTimeout(() => {
        s.destroy();
        done(new Error(`cache connection to ${this.opts.host}:${this.opts.port} timed out`));
      }, timeout);
      s.setNoDelay(true);
      s.once('error', (e) => done(e));
      s.once(this.opts.tls ? 'secureConnect' : 'connect', () => {
        s.removeAllListeners('error');
        this.attach(s);
        this.handshake(s).then(
          () => done(null, s),
          (e) => {
            s.destroy();
            done(e instanceof Error ? e : new Error(String(e)));
          },
        );
      });
    });
    return this.connecting;
  }

  private attach(s: net.Socket): void {
    this.socket = s;
    this.parser = new RespParser();
    s.on('data', (chunk: Buffer) => {
      this.parser.push(chunk);
      for (;;) {
        let reply: RespValue | RespError | undefined;
        try {
          reply = this.parser.next();
        } catch (e) {
          s.destroy(e instanceof Error ? e : new Error(String(e)));
          return;
        }
        if (reply === undefined) return;
        // A subscriber connection (onPush set) only receives pushes.
        if (this.onPush && Array.isArray(reply)) {
          this.onPush(String(reply[0]), String(reply[1]), String(reply[2] ?? ''));
          continue;
        }
        this.settle(reply);
      }
    });
    const drop = (err?: Error) => {
      if (this.socket !== s) return;
      this.socket = null;
      const pending = this.pending;
      this.pending = [];
      for (const p of pending) {
        clearTimeout(p.timer);
        p.reject(err || new Error('cache connection closed'));
      }
      if (!this.closed && this.onDisconnect) this.onDisconnect(err);
    };
    s.on('error', (e) => drop(e));
    s.on('close', () => drop());
  }

  private settle(reply: RespValue | RespError): void {
    const p = this.pending.shift();
    if (!p) return;
    clearTimeout(p.timer);
    if (reply instanceof RespError) p.reject(reply);
    else p.resolve(reply);
  }

  private async handshake(s: net.Socket): Promise<void> {
    const send = (args: string[]) => this.write(s, args);
    if (this.opts.password !== undefined) {
      await send(this.opts.username ? ['AUTH', this.opts.username, this.opts.password] : ['AUTH', this.opts.password]);
    }
    if (this.opts.db) await send(['SELECT', String(this.opts.db)]);
  }

  private write(s: net.Socket, args: Array<string | number | Buffer>): Promise<RespValue> {
    const timeout = this.opts.commandTimeoutMs ?? 1000;
    return new Promise<RespValue>((resolve, reject) => {
      const timer = setTimeout(() => {
        // The reply order is unknown after a timeout: drop the connection.
        s.destroy(new Error(`cache command ${String(args[0])} timed out after ${timeout}ms`));
      }, timeout);
      this.pending.push({ resolve, reject, timer });
      s.write(encodeCommand(args), (err) => {
        if (err) s.destroy(err);
      });
    });
  }

  /** Sends a command and resolves with its reply. */
  async command(...args: Array<string | number | Buffer>): Promise<RespValue> {
    if (this.closed) throw new Error('cache connection is closed');
    const s = await this.connect();
    return this.write(s, args);
  }

  /** Writes a command without waiting for a reply (pub/sub subscribe uses pushes). */
  async send(...args: Array<string | number | Buffer>): Promise<void> {
    const s = await this.connect();
    s.write(encodeCommand(args));
  }

  /** Opens the connection now (used by the subscriber to reconnect). */
  async ensure(): Promise<void> {
    await this.connect();
  }

  close(): void {
    this.closed = true;
    if (this.socket) this.socket.destroy();
    this.socket = null;
  }
}
