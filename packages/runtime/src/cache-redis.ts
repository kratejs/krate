// The Redis cache adapter: a RESP client over node:net (resp.ts), no dependency.
//
// Layout (prefix defaults to "krate:"):
//   <prefix>k:<key>      the value, with EX when it expires
//   <prefix>t:<tag>      a set of the keys stored with tag
//   <prefix>c:<channel>  pub/sub channel

import type { CacheAdapter, CacheSetOptions } from './cache-adapter.js';
import { RespConnection, parseRedisURL, type RespOptions, type RespValue } from './resp.js';

export interface RedisAdapterOptions {
  url: string;
  prefix?: string;
  /** Milliseconds one command may take (default 250). */
  timeoutMs?: number;
}

export class RedisAdapter implements CacheAdapter {
  readonly name = 'redis';
  private readonly conn: RespConnection;
  private readonly prefix: string;
  private readonly connOpts: RespOptions;
  private subscribers: Array<() => void> = [];

  constructor(options: RedisAdapterOptions) {
    this.connOpts = { ...parseRedisURL(options.url), commandTimeoutMs: options.timeoutMs ?? 250, connectTimeoutMs: Math.max(options.timeoutMs ?? 250, 500) };
    this.conn = new RespConnection(this.connOpts);
    this.prefix = options.prefix ?? 'krate:';
  }

  private k(key: string): string {
    return this.prefix + 'k:' + key;
  }

  private t(tag: string): string {
    return this.prefix + 't:' + tag;
  }

  async get(key: string): Promise<string | null> {
    const v = await this.conn.command('GET', this.k(key));
    return typeof v === 'string' ? v : null;
  }

  async set(key: string, value: string, options: CacheSetOptions = {}): Promise<void> {
    const ttl = options.ttlSeconds && options.ttlSeconds !== Infinity && options.ttlSeconds > 0 ? Math.ceil(options.ttlSeconds) : 0;
    const writes: Array<Promise<RespValue>> = [ttl ? this.conn.command('SET', this.k(key), value, 'EX', ttl) : this.conn.command('SET', this.k(key), value)];
    for (const tag of options.tags || []) {
      writes.push(this.conn.command('SADD', this.t(tag), this.k(key)));
      // A tag set outlives its newest member a little, so it never grows forever.
      if (ttl) writes.push(this.conn.command('EXPIRE', this.t(tag), ttl + 60));
    }
    await Promise.all(writes);
  }

  async delete(key: string): Promise<void> {
    await this.conn.command('DEL', this.k(key));
  }

  async keysByTag(tag: string): Promise<string[]> {
    const members = await this.conn.command('SMEMBERS', this.t(tag));
    const prefix = this.prefix + 'k:';
    return Array.isArray(members) ? members.filter((m): m is string => typeof m === 'string' && m.startsWith(prefix)).map((m) => m.slice(prefix.length)) : [];
  }

  async deleteByTags(tags: string[]): Promise<number> {
    let n = 0;
    for (const tag of tags) {
      const members = await this.conn.command('SMEMBERS', this.t(tag));
      const keys = Array.isArray(members) ? members.filter((m): m is string => typeof m === 'string') : [];
      if (keys.length > 0) {
        const removed = await this.conn.command('DEL', ...keys);
        if (typeof removed === 'number') n += removed;
      }
      await this.conn.command('DEL', this.t(tag));
    }
    return n;
  }

  async publish(channel: string, message: string): Promise<void> {
    await this.conn.command('PUBLISH', this.prefix + 'c:' + channel, message);
  }

  async subscribe(channel: string, handler: (message: string) => void): Promise<() => void> {
    const full = this.prefix + 'c:' + channel;
    const sub = new RespConnection({ ...this.connOpts, commandTimeoutMs: 5000 });
    let stopped = false;
    let delay = 200;
    const open = async (): Promise<void> => {
      await sub.ensure();
      await sub.send('SUBSCRIBE', full);
    };
    sub.onPush = (kind, ch, payload) => {
      if (kind === 'message' && ch === full) handler(payload);
    };
    sub.onDisconnect = () => {
      if (stopped) return;
      const retry = (): void => {
        if (stopped) return;
        open().then(
          () => {
            delay = 200;
            // Messages published while disconnected are gone: tell the caller.
            handler('*');
          },
          () => {
            delay = Math.min(delay * 2, 5000);
            setTimeout(retry, delay).unref?.();
          },
        );
      };
      setTimeout(retry, delay).unref?.();
    };
    await open();
    const stop = () => {
      stopped = true;
      sub.close();
    };
    this.subscribers.push(stop);
    return stop;
  }

  async close(): Promise<void> {
    for (const stop of this.subscribers) stop();
    this.subscribers = [];
    this.conn.close();
  }
}

/** createRedisAdapter({ url }) is `new RedisAdapter({ url })`, for passing to createKrateHandler({ cache }). */
export function createRedisAdapter(options: RedisAdapterOptions): RedisAdapter {
  return new RedisAdapter(options);
}
