// ImageResponse: JSX in, PNG out, for opengraph-image.tsx and twitter-image.tsx
// (the same shape as next/og). The element tree is serialized here and drawn by
// the Krate server in Go (internal/ogimage): there is no headless browser, no
// native module and no dependency. Only the subset documented in the Metadata
// routes guide is drawn; anything else is a build error that names it.
//
//   export default function Image() {
//     return new ImageResponse(<div style={{ fontSize: 96, background: "white" }}>Hello</div>, { width: 1200, height: 630 });
//   }

export interface ImageResponseFont {
  name: string;
  data: ArrayBuffer | ArrayBufferView;
  weight?: number;
  style?: 'normal' | 'italic';
}

export interface ImageResponseOptions {
  /** Width in pixels (default 1200). */
  width?: number;
  /** Height in pixels (default 630). */
  height?: number;
  /** TrueType or OpenType fonts to draw with (the built-in Go fonts cover Latin text). */
  fonts?: ImageResponseFont[];
  status?: number;
  statusText?: string;
  headers?: HeadersInit;
}

/** What the renderer takes: see internal/ogimage/spec.go. */
export interface ImageNode {
  type: 'element' | 'text';
  tag?: string;
  style?: Record<string, unknown>;
  src?: string;
  text?: string;
  children?: ImageNode[];
}

export interface ImageSpec {
  width: number;
  height: number;
  fonts?: Array<{ name: string; data: string; weight?: number; style?: string }>;
  tree: ImageNode;
}

const FRAGMENT_TYPES = new Set<unknown>();

function toBase64(data: ArrayBuffer | ArrayBufferView): string {
  const bytes = data instanceof ArrayBuffer ? new Uint8Array(data) : new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
  const B = (globalThis as { Buffer?: { from(b: Uint8Array): { toString(enc: string): string } } }).Buffer;
  if (B) return B.from(bytes).toString('base64');
  let s = '';
  for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i]);
  return btoa(s);
}

function textOf(v: unknown): string {
  return typeof v === 'number' ? String(v) : (v as string);
}

/** Serializes a JSX value to the renderer's tree. Function components are called. */
export function serializeElement(node: unknown, path = 'root'): ImageNode[] {
  if (node === null || node === undefined || node === false || node === true) return [];
  if (typeof node === 'string' || typeof node === 'number') return [{ type: 'text', text: textOf(node) }];
  if (Array.isArray(node)) return node.flatMap((c, i) => serializeElement(c, `${path}[${i}]`));
  if (typeof node !== 'object') throw new TypeError(`ImageResponse: ${path} is not an element`);
  const el = node as { type?: unknown; props?: Record<string, any>; children?: unknown[]; __raw?: string };
  if (typeof el.__raw === 'string') throw new TypeError(`ImageResponse: ${path} is raw HTML, which is not supported`);
  const props = { ...(el.props || {}) };
  const children = el.children || [];
  if (typeof el.type === 'function') {
    const out = (el.type as (p: Record<string, unknown>) => unknown)({ ...props, children: children.length === 1 ? children[0] : children });
    if (out && typeof (out as { then?: unknown }).then === 'function') {
      throw new TypeError(`ImageResponse: ${path} is an async component; resolve its data before building the image`);
    }
    return serializeElement(out, path);
  }
  if (typeof el.type !== 'string') {
    if (FRAGMENT_TYPES.has(el.type)) return serializeElement(children, path);
    throw new TypeError(`ImageResponse: ${path} has an unsupported element type`);
  }
  if (props.tw !== undefined) throw new TypeError(`ImageResponse: the tw prop is not supported (${path} <${el.type}>); use the style prop`);
  const style: Record<string, unknown> = { ...(typeof props.style === 'object' && props.style ? props.style : {}) };
  const out: ImageNode = { type: 'element', tag: el.type, style };
  if (el.type === 'img') {
    out.src = typeof props.src === 'string' ? props.src : '';
    if (props.width !== undefined && style.width === undefined) style.width = props.width;
    if (props.height !== undefined && style.height === undefined) style.height = props.height;
    return [out];
  }
  out.children = children.flatMap((c, i) => serializeElement(c, `${path}>${el.type}[${i}]`));
  return [out];
}

/** The renderer spec for an element and options. */
export function buildImageSpec(element: unknown, options: ImageResponseOptions = {}): ImageSpec {
  const nodes = serializeElement(element);
  if (nodes.length !== 1) throw new TypeError('ImageResponse expects one root element');
  const spec: ImageSpec = { width: options.width ?? 1200, height: options.height ?? 630, tree: nodes[0] };
  if (options.fonts && options.fonts.length) {
    spec.fonts = options.fonts.map((f) => ({ name: f.name, data: toBase64(f.data), weight: f.weight, style: f.style }));
  }
  return spec;
}

const MIME: Record<string, string> = { png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif', webp: 'image/webp' };

/**
 * Replaces the src of every <img> (a file path under root or public/, or an
 * http(s) URL) with a data: URI, so the Go renderer needs no network or file
 * access. Run at build time and before a per-request render.
 */
export async function inlineImages(spec: ImageSpec, root: string): Promise<void> {
  const visit = async (n: ImageNode): Promise<void> => {
    if (n.type !== 'element') return;
    if (n.tag === 'img' && n.src && !n.src.startsWith('data:')) n.src = await loadImage(n.src, root);
    for (const c of n.children || []) await visit(c);
  };
  await visit(spec.tree);
}

async function loadImage(src: string, root: string): Promise<string> {
  const ext = (src.split(/[?#]/)[0].split('.').pop() || 'png').toLowerCase();
  const mime = MIME[ext] || 'image/png';
  if (/^https?:\/\//i.test(src)) {
    const res = await fetch(src);
    if (!res.ok) throw new Error(`ImageResponse: ${src} answered ${res.status}`);
    return `data:${res.headers.get('content-type') || mime};base64,${toBase64(new Uint8Array(await res.arrayBuffer()))}`;
  }
  const fs = await import('node:fs/promises');
  const path = await import('node:path');
  const rel = src.replace(/^\//, '');
  for (const base of [path.join(root, 'public', rel), path.join(root, rel), src]) {
    try {
      return `data:${mime};base64,${toBase64(await fs.readFile(base))}`;
    } catch {
      /* try the next place */
    }
  }
  throw new Error(`ImageResponse: cannot find the image ${src} (looked in public/ and the project root)`);
}

function env(name: string): string {
  const proc = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process;
  return (proc && proc.env && proc.env[name]) || '';
}

/** Draws the spec on the Krate server (per-request path); the build calls the renderer directly. */
async function renderOnHost(spec: ImageSpec): Promise<Uint8Array> {
  const host = env('KRATE_HOST_URL');
  const secret = env('KRATE_INTERNAL_SECRET');
  if (!host || !secret) {
    throw new Error(
      'ImageResponse can only be drawn by the Krate server: at build time (opengraph-image files) or in a route served by `krate start` with the Node API runtime. This runtime has no renderer.',
    );
  }
  await inlineImages(spec, env('KRATE_ROOT') || '.');
  const res = await fetch(host.replace(/\/$/, '') + '/__krate/internal/image', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-krate-internal': secret },
    body: JSON.stringify(spec),
  });
  if (!res.ok) throw new Error(`ImageResponse: the renderer answered ${res.status}: ${await res.text()}`);
  return new Uint8Array(await res.arrayBuffer());
}

/**
 * A Response that carries a PNG. Built from JSX with the same options as
 * Next.js's ImageResponse (width, height, fonts, status, headers).
 */
export class ImageResponse extends Response {
  /** The renderer spec; the build reads it instead of the body. */
  readonly __krateImage: ImageSpec;

  constructor(element: unknown, options: ImageResponseOptions = {}) {
    const spec = buildImageSpec(element, options);
    const body = new ReadableStream<Uint8Array>({
      async pull(controller) {
        try {
          controller.enqueue(await renderOnHost(spec));
          controller.close();
        } catch (err) {
          controller.error(err);
        }
      },
    });
    const headers = new Headers(options.headers);
    if (!headers.has('content-type')) headers.set('content-type', 'image/png');
    if (!headers.has('cache-control')) headers.set('cache-control', 'public, max-age=31536000, immutable');
    super(body, { status: options.status ?? 200, statusText: options.statusText, headers });
    this.__krateImage = spec;
  }
}
