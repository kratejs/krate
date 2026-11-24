// Krate Native helpers: the frame stream, the Host feed and the Memory tab.

/**
 * Splits a buffer of Krate frames (u32 length, JSON, u16 blob count, blobs)
 * into its complete JSON values. `rest` is the incomplete tail to prepend to
 * the next chunk. Exported (pure) for testing.
 */
export function parseFrames(buf: Uint8Array): { values: unknown[]; rest: Uint8Array } {
  const values: unknown[] = [];
  const view = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);
  const td = new TextDecoder();
  let pos = 0;
  while (buf.length - pos >= 4) {
    const n = view.getUint32(pos);
    let end = pos + 4 + n;
    if (buf.length < end + 2) break;
    const json = td.decode(buf.subarray(pos + 4, end));
    const blobs = view.getUint16(end);
    end += 2;
    let complete = true;
    for (let i = 0; i < blobs; i++) {
      if (buf.length < end + 4) {
        complete = false;
        break;
      }
      end += 4 + view.getUint32(end);
      if (buf.length < end) {
        complete = false;
        break;
      }
    }
    if (!complete) break;
    try {
      values.push(JSON.parse(json));
    } catch {
      /* skip a malformed frame */
    }
    pos = end;
  }
  return { values, rest: buf.subarray(pos) };
}

/** The host-side stack a KrateError carries in dev (`data.stack`), or "". */
export function hostStackOf(reason: unknown): string {
  const data = (reason as { data?: unknown } | null)?.data;
  const stack = data && typeof data === 'object' ? (data as { stack?: unknown }).stack : undefined;
  return typeof stack === 'string' ? stack : '';
}

export interface HostEvent {
  seq: number;
  kind: string;
  fn: string;
  id?: number;
  time?: string;
  durNs?: number;
  code?: string;
  message?: string;
}

/** One line of the Host panel. Exported (pure) for testing. */
export function hostEventLine(ev: HostEvent): string {
  const t = ev.time ? new Date(ev.time).toLocaleTimeString() : '';
  const dur = ev.durNs ? (ev.durNs / 1e6 >= 10 ? Math.round(ev.durNs / 1e6) : Math.round(ev.durNs / 1e5) / 10) + 'ms' : '';
  const err = ev.code ? ' ' + ev.code + (ev.message ? ': ' + ev.message : '') : '';
  return [t, ev.kind, ev.fn, dur].filter(Boolean).join(' ') + err;
}

/** The memstat report the shell serves at /__krate/dev/memory (bytes). */
export interface MemoryReport {
  os?: string;
  source?: string;
  procs?: { pid: number; name?: string; role: string; rss?: number; pss: number; privateDirty: number }[];
  total?: { pss: number; privateDirty: number; rss?: number };
  notes?: string[];
}

/** Format a byte count for the Memory panel. Exported (pure) for testing. */
export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return 'n/a';
  if (n >= 1 << 30) return (n / (1 << 30)).toFixed(2) + ' GiB';
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + ' MiB';
  if (n >= 1 << 10) return Math.round(n / (1 << 10)) + ' KiB';
  return Math.round(n) + ' B';
}

/**
 * The JS heap of this page, or null where the engine does not expose it.
 * Chromium has performance.memory; WebKit (the engine of the Linux and macOS
 * webviews) has no heap size API, so the panel shows the web process row
 * (PSS and private dirty, which include the JS heap) instead.
 */
export function jsHeapBytes(): number | null {
  const m = (typeof performance !== 'undefined' ? (performance as any).memory : undefined) as { usedJSHeapSize?: number } | undefined;
  return m && typeof m.usedJSHeapSize === 'number' ? m.usedJSHeapSize : null;
}

/** The rows of the Memory panel as [label, value] pairs. Exported (pure) for testing. */
export function memoryRows(rep: MemoryReport | null, domNodes: number, heap: number | null): [string, string][] {
  const rows: [string, string][] = [];
  if (rep && rep.procs) {
    for (const p of rep.procs) {
      rows.push([p.role + ' (' + p.pid + ')', 'PSS ' + formatBytes(p.pss) + ' / private dirty ' + formatBytes(p.privateDirty)]);
    }
    if (rep.total) rows.push(['total', 'PSS ' + formatBytes(rep.total.pss) + ' / private dirty ' + formatBytes(rep.total.privateDirty)]);
  } else {
    rows.push(['processes', 'unavailable']);
  }
  rows.push(['DOM nodes', String(domNodes)]);
  rows.push(['JS heap', heap === null ? 'unavailable in this engine (see the web process row)' : formatBytes(heap)]);
  return rows;
}
