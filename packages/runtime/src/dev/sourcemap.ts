// Source-map symbolication. When the build enables source maps the overlay
// decodes them so runtime stack traces point at real source locations instead
// of bundle offsets. Fetches fail silently when maps are absent.

interface MapSegment {
  genCol: number;
  src: number;
  srcLine: number;
  srcCol: number;
}

interface DecodedMap {
  sources: string[];
  lines: MapSegment[][];
}

const B64_VLQ = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';

/** Decodes a Base64 VLQ mapping segment into a delta array. */
export function decodeVLQ(str: string): number[] {
  const out: number[] = [];
  let shift = 0;
  let value = 0;
  for (let i = 0; i < str.length; i++) {
    const digit = B64_VLQ.indexOf(str[i]);
    if (digit === -1) continue;
    const cont = (digit & 32) !== 0;
    value += (digit & 31) << shift;
    if (cont) {
      shift += 5;
      continue;
    }
    const neg = (value & 1) === 1;
    value >>= 1;
    out.push(neg ? -value : value);
    value = 0;
    shift = 0;
  }
  return out;
}

/** Decodes a v3 `mappings` string into per-generated-line segments. */
export function decodeMappings(mappings: string): MapSegment[][] {
  const lines: MapSegment[][] = [];
  let src = 0;
  let srcLine = 0;
  let srcCol = 0;
  for (const line of mappings.split(';')) {
    const segs: MapSegment[] = [];
    let genCol = 0;
    if (line) {
      for (const part of line.split(',')) {
        if (!part) continue;
        const nums = decodeVLQ(part);
        genCol += nums[0] || 0;
        if (nums.length === 1) {
          // An end marker: no original position from here on.
          segs.push({ genCol, src: -1, srcLine: 0, srcCol: 0 });
        } else if (nums.length >= 4) {
          src += nums[1];
          srcLine += nums[2];
          srcCol += nums[3];
          segs.push({ genCol, src, srcLine, srcCol });
        }
      }
    }
    lines.push(segs);
  }
  return lines;
}

const mapCache = new Map<string, Promise<DecodedMap | null>>();

async function loadSourceMap(url: string): Promise<DecodedMap | null> {
  const cached = mapCache.get(url);
  if (cached) return cached;
  const p = (async () => {
    try {
      const u = new URL(url, location.href);
      if (u.protocol !== location.protocol || u.host !== location.host) return null;
      const res = await fetch(url + '.map');
      if (!res.ok) return null;
      const json = await res.json();
      if (!json || typeof json.mappings !== 'string') return null;
      return { sources: (json.sources as string[]) || [], lines: decodeMappings(json.mappings) } as DecodedMap;
    } catch {
      return null;
    }
  })();
  mapCache.set(url, p);
  return p;
}

interface StackFrame {
  prefix: string;
  url: string;
  line: number;
  col: number;
  suffix: string;
}

/** Parses a V8/Firefox stack line into its script location, if present. */
export function parseStackFrame(line: string): StackFrame | null {
  const patterns = [
    /^(.*?)((?:[a-z][a-z0-9+.-]*:)?\/\/[^\s)]+|\/[^\s):]+):(\d+):(\d+)(.*)$/,
  ];
  for (const re of patterns) {
    const m = re.exec(line);
    if (!m) continue;
    return { prefix: m[1], url: m[2], line: Number(m[3]), col: Number(m[4]), suffix: m[5] };
  }
  return null;
}

function remapLocation(map: DecodedMap, line: number, col: number): { source: string; line: number; col: number } | null {
  let best: MapSegment | null = null;
  const segs = map.lines[line - 1];
  if (segs && segs.length > 0) {
    for (const s of segs) {
      if (s.genCol <= col - 1) best = s;
      else break;
    }
    if (!best) best = segs[0];
  } else {
    // An unminified hydration map has a mapping only where a marker starts;
    // it holds until the next one, so a line without any takes the last
    // mapping before it.
    for (let i = Math.min(line - 1, map.lines.length) - 1; i >= 0 && !best; i--) {
      const prev = map.lines[i];
      if (prev && prev.length > 0) best = prev[prev.length - 1];
    }
    if (!best) return null;
  }
  if (best.src < 0) return null;
  const source = map.sources[best.src] || '';
  return { source, line: best.srcLine + 1, col: best.srcCol + 1 };
}

/** Rewrites each mappable frame of a stack trace to its original location. */
export async function symbolicateStack(stack: string): Promise<string> {
  if (!stack || !stack.includes('.js')) return stack;
  const lines = stack.split('\n');
  const out = await Promise.all(
    lines.map(async (line) => {
      const frame = parseStackFrame(line);
      if (!frame) return line;
      const map = await loadSourceMap(frame.url);
      if (!map) return line;
      const loc = remapLocation(map, frame.line, frame.col);
      // The generic label of a compiler-generated bundle says nothing more
      // than the frame's own file and line.
      if (!loc || !loc.source || loc.source === 'krate-hydration.js') return line;
      return frame.prefix + loc.source + ':' + loc.line + ':' + loc.col + frame.suffix;
    }),
  );
  return out.join('\n');
}
