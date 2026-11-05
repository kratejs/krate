// A tiny in-process RESP server for tests: the subset of Redis the cache adapter
// uses (strings, sets, expiry, pub/sub). It can be killed and revived on its port.

import net from 'node:net';
import { RespParser } from './resp.js';

export interface FakeRedis {
  readonly port: number;
  /** Drops every connection and stops listening (a Redis that died). */
  kill(): Promise<void>;
  /** Listens again on the same port. */
  revive(): Promise<void>;
  keys(): string[];
  commands: string[][];
}

export async function startFakeRedis(opts: { password?: string } = {}): Promise<FakeRedis> {
  const strings = new Map<string, string>();
  const sets = new Map<string, Set<string>>();
  const expires = new Map<string, number>();
  const subs = new Map<string, Set<net.Socket>>();
  const sockets = new Set<net.Socket>();
  const commands: string[][] = [];
  let server: net.Server;
  let port = 0;

  const alive = (k: string) => {
    const e = expires.get(k);
    if (e !== undefined && e <= Date.now()) {
      strings.delete(k);
      sets.delete(k);
      expires.delete(k);
    }
  };
  const bulk = (v: string | null) => (v === null ? '$-1\r\n' : `$${Buffer.byteLength(v)}\r\n${v}\r\n`);
  const arr = (items: string[]) => `*${items.length}\r\n${items.map((i) => bulk(i)).join('')}`;

  function handle(sock: net.Socket, args: string[]): string {
    commands.push(args);
    const cmd = args[0].toUpperCase();
    switch (cmd) {
      case 'AUTH':
        return opts.password !== undefined && args[args.length - 1] !== opts.password ? '-WRONGPASS invalid password\r\n' : '+OK\r\n';
      case 'SELECT':
      case 'PING':
        return '+OK\r\n';
      case 'GET':
        alive(args[1]);
        return bulk(strings.has(args[1]) ? strings.get(args[1])! : null);
      case 'SET':
        strings.set(args[1], args[2]);
        expires.delete(args[1]);
        if (args[3] && args[3].toUpperCase() === 'EX') expires.set(args[1], Date.now() + parseInt(args[4], 10) * 1000);
        return '+OK\r\n';
      case 'DEL': {
        let n = 0;
        for (const k of args.slice(1)) {
          alive(k);
          if (strings.delete(k)) n++;
          if (sets.delete(k)) n++;
        }
        return `:${n}\r\n`;
      }
      case 'SADD': {
        let s = sets.get(args[1]);
        if (!s) sets.set(args[1], (s = new Set()));
        let n = 0;
        for (const m of args.slice(2)) {
          if (!s.has(m)) {
            s.add(m);
            n++;
          }
        }
        return `:${n}\r\n`;
      }
      case 'SMEMBERS':
        alive(args[1]);
        return arr([...(sets.get(args[1]) || [])]);
      case 'EXPIRE':
        expires.set(args[1], Date.now() + parseInt(args[2], 10) * 1000);
        return ':1\r\n';
      case 'PUBLISH': {
        const targets = subs.get(args[1]) || new Set<net.Socket>();
        for (const t of targets) t.write(`*3\r\n${bulk('message')}${bulk(args[1])}${bulk(args[2])}`);
        return `:${targets.size}\r\n`;
      }
      case 'SUBSCRIBE': {
        let s = subs.get(args[1]);
        if (!s) subs.set(args[1], (s = new Set()));
        s.add(sock);
        return `*3\r\n${bulk('subscribe')}${bulk(args[1])}:1\r\n`;
      }
      default:
        return `-ERR unknown command ${cmd}\r\n`;
    }
  }

  function listen(): Promise<void> {
    return new Promise((resolve) => {
      server = net.createServer((sock) => {
        sockets.add(sock);
        const parser = new RespParser();
        sock.on('data', (chunk) => {
          parser.push(chunk as Buffer);
          for (;;) {
            const cmd = parser.next();
            if (cmd === undefined) return;
            sock.write(handle(sock, (cmd as unknown[]).map(String)));
          }
        });
        sock.on('close', () => {
          sockets.delete(sock);
          for (const s of subs.values()) s.delete(sock);
        });
        sock.on('error', () => {});
      });
      server.listen(port, '127.0.0.1', () => {
        port = (server.address() as net.AddressInfo).port;
        resolve();
      });
    });
  }
  await listen();

  const stop = (): Promise<void> =>
    new Promise((resolve) => {
      for (const s of sockets) s.destroy();
      server.close(() => resolve());
    });
  return {
    get port() {
      return port;
    },
    kill: stop,
    revive: listen,
    keys: () => [...strings.keys()],
    commands,
  };
}
