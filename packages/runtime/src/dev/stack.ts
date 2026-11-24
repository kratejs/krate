// Stack trace parsing for the runtime error view: frames, and the split into
// the app's own frames and folded framework frames.

export interface Frame {
  fn: string;
  file: string;
  line: number;
  col: number;
  raw: string;
  app: boolean;
  /** The location is a URL the page loaded (no source map), not a project file. */
  served: boolean;
}

export type StackItem = { type: 'app'; frame: Frame } | { type: 'fold'; frames: Frame[] };

const V8_NAMED = /^\s*at (?:async )?(.*?) \((.*?):(\d+):(\d+)\)\s*$/;
const V8_ANON = /^\s*at (?:async )?(.*?):(\d+):(\d+)\s*$/;
const GECKO = /^\s*(.*?)@(.*?):(\d+):(\d+)\s*$/;

/** True for frames of the runtime, generated chunks, the overlay or dependencies. */
export function isFrameworkFile(file: string): boolean {
  return (
    !file ||
    /node_modules|@krate\/|\/chunks\/|^chunks\/|krate-[a-z-]+\.js|__krate\/|<anonymous>|native code|packages\/runtime\/|krate-hydration\.js|^(?:node|internal):/.test(file)
  );
}

/** A file shown relative to the page origin. */
export function displayFile(file: string): string {
  try {
    if (/^[a-z][a-z0-9+.-]*:\/\//i.test(file)) {
      const u = new URL(file);
      if (typeof location !== 'undefined' && u.origin === location.origin) return u.pathname.replace(/^\//, '');
      return u.host + u.pathname;
    }
  } catch {
    /* keep it as is */
  }
  return file.replace(/^\.\//, '');
}

export function parseFrame(line: string): Frame | null {
  let m = V8_NAMED.exec(line);
  let fn = '';
  let file = '';
  let ln = 0;
  let col = 0;
  if (m) {
    [, fn, file] = m;
    ln = Number(m[3]);
    col = Number(m[4]);
  } else if ((m = V8_ANON.exec(line))) {
    file = m[1];
    ln = Number(m[2]);
    col = Number(m[3]);
  } else if ((m = GECKO.exec(line))) {
    [, fn, file] = m;
    ln = Number(m[3]);
    col = Number(m[4]);
  } else {
    return null;
  }
  const served = /^[a-z][a-z0-9+.-]*:\/\//i.test(file.trim());
  file = displayFile(file.trim());
  return { fn: fn.trim() || '<anonymous>', file, line: ln, col, raw: line.trim(), app: !isFrameworkFile(file), served };
}

export function parseStack(stack: string | undefined): Frame[] {
  if (!stack) return [];
  const out: Frame[] = [];
  for (const line of stack.split('\n')) {
    if (/^\s*Host stack:/.test(line)) break;
    const f = parseFrame(line);
    if (f) out.push(f);
  }
  return out;
}

/** App frames stay as they are; each run of framework frames folds into one item. */
export function splitStack(frames: Frame[]): StackItem[] {
  const items: StackItem[] = [];
  for (const f of frames) {
    if (f.app) {
      items.push({ type: 'app', frame: f });
      continue;
    }
    const last = items[items.length - 1];
    if (last && last.type === 'fold') last.frames.push(f);
    else items.push({ type: 'fold', frames: [f] });
  }
  return items;
}

/** The first frame in the app's own code, or null. */
export function firstAppFrame(frames: Frame[]): Frame | null {
  return frames.find((f) => f.app) || null;
}

/** The error class named on the first line of a stack ("TypeError"), else "". */
export function errorName(stack: string | undefined, message = ''): string {
  const first = (stack || '').split('\n')[0] || '';
  const m = /^\s*([A-Z][A-Za-z0-9_$]*(?:Error|Exception))\b/.exec(first) || /^\s*([A-Z][A-Za-z0-9_$]*(?:Error|Exception)):/.exec(message);
  return m ? m[1] : '';
}

/**
 * The first project location in a server stack or log: `src/x.ts:3:15`, or
 * an absolute path outside node_modules. Returns null when there is none.
 */
export function projectLocation(text: string | undefined): { file: string; line: number; col: number } | null {
  if (!text) return null;
  // Page URLs in the log tail (browser reports) are not server locations.
  text = text
    .split('\n')
    .filter((l) => !/^\s*\[browser\]/.test(l))
    .join('\n')
    .replace(/[a-z][a-z0-9+.-]*:\/\/\S+/gi, '');
  const re = /((?:\/|[A-Za-z]:\\|\.\/)?(?:[\w@.-]+[\\/])*[\w@.[\]-]+\.(?:tsx|ts|jsx|js|mjs|cjs|mts|mdx|go)):(\d+)(?::(\d+))?/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text))) {
    const file = m[1];
    if (/node_modules|\.krate[\\/]|^node:|chunks[\\/]|[\\/]dist[\\/]/.test(file)) continue;
    return { file, line: Number(m[2]), col: Number(m[3] || 1) };
  }
  return null;
}
