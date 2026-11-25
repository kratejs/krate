// Failed network requests of the page (fetch and XMLHttpRequest), kept in a
// small ring so a runtime error can show the requests around it.

export interface FailedRequest {
  method: string;
  url: string;
  /** HTTP status, 0 for a network error. */
  status: number;
  /** The network error's message, when the request did not complete. */
  error?: string;
  /** Duration in ms. */
  ms: number;
  /** When the request ended (Date.now()). */
  t: number;
}

export const REQUEST_RING = 50;

let unwrapped: typeof fetch | undefined;

/**
 * fetch without the capture: the overlay's own requests (source maps, code
 * frames, a stylesheet swap) are not the page's and must not be recorded.
 */
export function rawFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  return (unwrapped || fetch)(input, init);
}

/** How far before an error a failed request still counts as near it. */
export const NEAR_BEFORE_MS = 15000;
const NEAR_AFTER_MS = 2000;

export class RequestLog {
  list: FailedRequest[] = [];
  onChange?: () => void;

  add(r: FailedRequest): void {
    this.list.push(r);
    if (this.list.length > REQUEST_RING) this.list.shift();
    this.onChange?.();
  }

  /** The failed requests that ended shortly before (or just after) time t, newest last. */
  near(t: number): FailedRequest[] {
    return this.list.filter((r) => r.t >= t - NEAR_BEFORE_MS && r.t <= t + NEAR_AFTER_MS);
  }
}

/** True for the overlay's own requests and other dev server internals. */
export function isInternalURL(url: string): boolean {
  try {
    const u = new URL(url, typeof location !== 'undefined' ? location.href : 'http://localhost/');
    return /^\/__krate\//.test(u.pathname);
  } catch {
    return false;
  }
}

function urlOf(input: unknown): string {
  if (typeof input === 'string') return input;
  if (input && typeof input === 'object') {
    const o = input as { url?: string; href?: string };
    if (typeof o.url === 'string') return o.url;
    if (typeof o.href === 'string') return o.href;
  }
  return String(input);
}

/** A request URL shown relative to the page origin when it is same-origin. */
export function displayURL(url: string): string {
  try {
    const u = new URL(url, location.href);
    return u.origin === location.origin ? u.pathname + u.search : u.href;
  } catch {
    return url;
  }
}

/**
 * Wraps fetch and XMLHttpRequest so failed requests (a status of 400 or more,
 * or a network error) land in log. Returns the function that undoes it.
 */
export function captureRequests(log: RequestLog, g: any = globalThis): () => void {
  const undo: (() => void)[] = [];
  const now = () => (typeof performance !== 'undefined' ? performance.now() : Date.now());
  const origFetch = g.fetch;
  if (typeof origFetch === 'function') {
    unwrapped = origFetch.bind(g);
    const wrapped = function (this: unknown, input: unknown, init?: { method?: string }) {
      const url = urlOf(input);
      const method = String(init?.method || (input && typeof input === 'object' && (input as any).method) || 'GET').toUpperCase();
      const start = now();
      const p = origFetch.apply(this, arguments as any) as Promise<Response>;
      if (isInternalURL(url)) return p;
      return p.then(
        (res: Response) => {
          try {
            if (res && res.status >= 400) log.add({ method, url, status: res.status, ms: Math.round(now() - start), t: Date.now() });
          } catch {
            /* never break the page's request */
          }
          return res;
        },
        (err: unknown) => {
          try {
            const aborted = err && typeof err === 'object' && (err as any).name === 'AbortError';
            if (!aborted) log.add({ method, url, status: 0, error: err instanceof Error ? err.message : String(err), ms: Math.round(now() - start), t: Date.now() });
          } catch {
            /* never break the page's request */
          }
          throw err;
        },
      );
    };
    g.fetch = wrapped;
    undo.push(() => {
      if (g.fetch === wrapped) g.fetch = origFetch;
      unwrapped = undefined;
    });
  }
  const XHR = g.XMLHttpRequest;
  if (typeof XHR === 'function' && XHR.prototype) {
    const proto = XHR.prototype;
    const open = proto.open;
    const send = proto.send;
    proto.open = function (this: any, method: string, url: string | URL) {
      this.__kdReq = { method: String(method || 'GET').toUpperCase(), url: urlOf(url) };
      return open.apply(this, arguments as any);
    };
    proto.send = function (this: any) {
      const req = this.__kdReq as { method: string; url: string } | undefined;
      if (req && !isInternalURL(req.url)) {
        const start = now();
        let done = false;
        const finish = (error?: string) => {
          if (done) return;
          done = true;
          try {
            const status = error ? 0 : Number(this.status) || 0;
            if (error || status >= 400 || status === 0) {
              log.add({ method: req.method, url: req.url, status, error: error || (status === 0 ? 'network error' : undefined), ms: Math.round(now() - start), t: Date.now() });
            }
          } catch {
            /* never break the page's request */
          }
        };
        this.addEventListener('load', () => finish());
        this.addEventListener('error', () => finish('network error'));
        this.addEventListener('timeout', () => finish('timed out'));
      }
      return send.apply(this, arguments as any);
    };
    undo.push(() => {
      proto.open = open;
      proto.send = send;
    });
  }
  return () => {
    for (const u of undo.splice(0)) u();
  };
}
