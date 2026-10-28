// @krate/runtime/server - Server-side rendering runtime
// Provides the same API as the client runtime but renders to HTML strings.

export { getActionContext, headers, cookies, redirect } from './action-context.js';
export type { ActionContext, ActionCookies, ActionCookieOptions } from './action-context.js';
export { revalidatePath, revalidateTag, unstable_cache, unstable_noStore } from './cache.js';
export type { RevalidatePathType, UnstableCacheOptions } from './cache.js';

// Signal (server: returns initial value, no reactivity)

export function createSignal<T>(initial: T): [() => T, (v: T | ((prev: T) => T)) => void] {
  let value = initial;
  const read = () => value;
  const write = (v: T | ((prev: T) => T)) => {
    value = typeof v === "function" ? (v as (prev: T) => T)(value) : v;
  };
  return [read, write];
}

export function createEffect(_fn: () => void): () => void {
  // No-op on server - effects only run on client
  return () => {};
}

export function createMemo<T>(fn: () => T): () => T {
  let cached: T;
  let dirty = true;
  return () => {
    if (dirty) {
      cached = fn();
      dirty = false;
    }
    return cached;
  };
}

// Derived render values (krateDerived) and custom hook results (krateHook)
// have no dependencies to track on the server: each read recomputes.
const derivedGetters = new WeakSet<object>();

export function krateDerived<T>(fn: () => T, _triggers?: () => unknown): () => T {
  const read = () => fn();
  derivedGetters.add(read);
  return read;
}

export function krateHook<T>(value: T | (() => T)): () => T {
  if (typeof value === "function" && derivedGetters.has(value)) return value as () => T;
  return () => value as T;
}

export function onCleanup(_fn: () => void): void {
  // No-op on server
}

export function onMount(_fn: () => void): void {
  // No-op on server: onMount callbacks (DOM setup, WebGL renderers, timers) are
  // client-only. Running them during SSR would touch a DOM that doesn't exist
  // and crash.
}

// JSX Runtime (server: renders to HTML strings)

interface RawHTML { __raw: string; }
function raw(html: string): RawHTML { return { __raw: html }; }

// goComponent is the SSR bridge for Go-authored components (see the compiler's
// server-bundle esbuild plugin). A server-tier component inlines its build-time
// baked HTML when the call site was annotated with __krateGoId; otherwise (and
// always for runtime-tier) it emits a marker the Go server resolves by calling
// the Go host at request time. revalidate is the component file's cache window,
// carried in the marker so the Go server needn't look it up by name.
export function goComponent(
  name: string,
  tier: string,
  baked: Record<string, string> | null | undefined,
  props: Record<string, any> | undefined,
  revalidate = 0
): JSXNode {
  const p = props || {};
  const id = p.__krateGoId as string | undefined;
  if (tier === "server" && baked && id && Object.prototype.hasOwnProperty.call(baked, id)) {
    return raw(baked[id]);
  }
  const rest: Record<string, any> = {};
  for (const k in p) {
    if (k !== "__krateGoId" && k !== "children") rest[k] = p[k];
  }
  if (p.children != null) {
    const kids = renderToString(p.children);
    if (kids !== "") rest["children"] = kids;
  }
  return raw("<!--krate-go:" + name + ":" + (revalidate > 0 ? revalidate : 0) + ":" + base64url(JSON.stringify(rest)) + "-->");
}

function base64url(s: string): string {
  const bytes = new TextEncoder().encode(s);
  let bin = "";
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

type JSXNode = string | number | boolean | null | undefined | JSXNode[] | JSXElement | RawHTML;

interface JSXElement {
  type: string | Function;
  props: Record<string, any>;
  children: JSXNode[];
}

export function Fragment(_props: { children?: JSXNode }): JSXNode {
  return _props.children ?? [];
}

export function jsx(type: string | Function, props: Record<string, any>, _key?: string): JSXElement {
  const { children, ...rest } = props ?? {};
  const childArray = Array.isArray(children)
    ? children.flat(Infinity)
    : children != null
    ? [children]
    : [];
  return { type, props: rest, children: childArray };
}

export const jsxs = jsx;

// HTML Rendering

const VOID_ELEMENTS = new Set([
  "area", "base", "br", "col", "embed", "hr", "img", "input",
  "link", "meta", "param", "source", "track", "wbr",
]);

function escapeHTML(str: string): string {
  return str
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function renderNode(node: JSXNode): string {
  if (node == null || node === false) return "";
  if (typeof node === "string") return escapeHTML(node);
  if (typeof node === "number") return String(node);
  if (typeof node === "boolean") return "";
  if (Array.isArray(node)) return node.map(renderNode).join("");
  if (typeof node === "object" && "__raw" in node) return (node as RawHTML).__raw;
  if (typeof node === "object" && "type" in node) return renderElement(node as JSXElement);
  return "";
}

function isServerAction(v: any): v is { actionUrl: string } {
  return typeof v === "function" && typeof (v as any).actionUrl === "string";
}

function setAttr(key: string, val: any): string {
  if (val == null || val === false) return "";
  if (val === true) return ` ${key}`;
  if (key === "action" && isServerAction(val)) {
    return ` action="${escapeHTML(val.actionUrl)}" data-krate-action`;
  }
  if ((key === "formAction" || key === "formaction") && isServerAction(val)) {
    return ` formaction="${escapeHTML(val.actionUrl)}" formmethod="post" data-krate-action`;
  }
  // Client function actions run only after hydration; they have no HTML form.
  if (typeof val === "function") return "";
  if (key === "class") return ` class="${escapeHTML(String(val))}"`;
  if (key === "style" && typeof val === "object") {
    const css = Object.entries(val)
      .map(([k, v]) => `${k}:${v}`)
      .join(";");
    return ` style="${escapeHTML(css)}"`;
  }
  if (key.startsWith("on") || key.startsWith("__")) return "";
  return ` ${key}="${escapeHTML(String(val))}"`;
}

export function renderToString(node: JSXNode): string {
  return renderNode(node);
}

function renderElement(el: JSXElement): string {
  const { type, props, children } = el;

  // Component
  if (typeof type === "function") {
    const result = type({ ...props, children });
    return renderNode(result);
  }

  // HTML element
  const tag = type;
  const renderProps =
    tag === "form" && isServerAction(props.action) && props.method == null
      ? { ...props, method: "post" }
      : props;
  const attrs = Object.entries(renderProps)
    .map(([k, v]) => setAttr(k, v))
    .join("");

  if (VOID_ELEMENTS.has(tag)) {
    return `<${tag}${attrs}>`;
  }

  const inner = children.map(renderNode).join("");
  return `<${tag}${attrs}>${inner}</${tag}>`;
}

// Streaming SSR via Suspense

// Use globalThis for boundary counter so it's shared between the unbundled
// server-renderer and esbuild-bundled page components (each gets its own copy).
export function setStreamingResolved(v: boolean) {
  (globalThis as any).__krate_streaming_resolved = v;
}
export function resetBoundaryCounter() {
  (globalThis as any).__krate_boundary_counter = 0;
}

function nextBoundaryId(): number {
  const g = globalThis as any;
  if (g.__krate_boundary_counter == null) g.__krate_boundary_counter = 0;
  return g.__krate_boundary_counter++;
}

interface SuspenseProps {
  fallback: JSXNode;
  children: JSXNode;
}

// Suspense component for streaming SSR.
// Phase 1 (resolved=false): renders fallback in a <span>, children placeholder in a <template>
// Phase 2 (resolved=true): renders just the raw children with extraction markers
export function Suspense(props: SuspenseProps): JSXNode {
  const id = nextBoundaryId();
  if ((globalThis as any).__krate_streaming_resolved) {
    // Phase 2: output resolved children wrapped in markers for extraction
    const inner = renderNode(props.children);
    return raw(`<!--suspense-resolved:${id}-->${inner}<!--/suspense-resolved:${id}-->`);
  }
  const inner = renderNode(props.children);
  const fallback = renderNode(props.fallback);
  return raw(
    `<span data-suspense="${id}">${fallback}</span>` +
    `<template data-suspense="${id}">${inner}</template>`
  );
}

// Built-in Components (server stubs)

// Head - renders children into the page head at build time.
// On the server, we just render children; the build pipeline extracts them.
export function Head(props: { children?: JSXNode }): JSXNode {
  return props.children ?? "";
}

// Script - renders a <script> tag.
export function Script(props: { src?: string; children?: string }): JSXNode {
  if (props.src) {
    return `<script src="${escapeHTML(props.src)}"></script>`;
  }
  return `<script>${props.children ?? ""}</script>`;
}

// Style - renders a <style> tag.
export function Style(props: { children?: string }): JSXNode {
  return `<style>${props.children ?? ""}</style>`;
}

// Link - renders an <a> tag wired for SPA navigation (server renderer).
export function Link(props: {
  href?: string;
  prefetch?: boolean;
  replace?: boolean;
  scroll?: boolean;
  children?: JSXNode;
  [key: string]: any;
}): JSXNode {
  const { href = "", prefetch, replace, scroll, children, ...rest } = props;
  const attrs: string[] = [];
  if (href) attrs.push(`href="${escapeHTML(href)}"`);

  const external =
    /^(https?:|mailto:|tel:|#|javascript:)/.test(href) ||
    href.startsWith("//") ||
    rest.target === "_blank" ||
    rest.download !== undefined;

  if (external) {
    attrs.push("data-krate-external");
  } else {
    attrs.push("data-krate-link");
    if (prefetch !== false) attrs.push("data-prefetch");
    if (replace) attrs.push("data-krate-replace");
    if (scroll === false) attrs.push('data-krate-scroll="false"');
  }

  for (const key of ["target", "rel", "title", "id", "aria-label"]) {
    const v = rest[key];
    if (v !== undefined && v !== null) attrs.push(`${key}="${escapeHTML(String(v))}"`);
  }
  if (rest.target === "_blank" && rest.rel === undefined) attrs.push('rel="noopener noreferrer"');
  if (rest.className) attrs.push(`class="${escapeHTML(String(rest.className))}"`);

  const inner = children != null ? renderNode(children) : "";
  return raw(`<a ${attrs.join(" ")}>${inner}</a>`);
}

// Image - renders an <img> tag (server stub; Go compiler handles full responsive output at build time).
export function Image(props: { src?: string; alt?: string; width?: number; height?: number; [key: string]: any }): JSXNode {
  const { src = "", alt = "", width, height, ...rest } = props;
  const attrs: string[] = [];
  if (src) attrs.push(`src="${escapeHTML(src)}"`);
  if (alt) attrs.push(`alt="${escapeHTML(alt)}"`);
  if (width) attrs.push(`width="${width}"`);
  if (height) attrs.push(`height="${height}"`);
  return raw(`<img ${attrs.join(" ")} />`);
}

// Icon - renders an empty span placeholder (Go compiler handles SVG fetching at build time).
export function Icon(_props: { name?: string; [key: string]: any }): JSXNode {
  return raw("<span></span>");
}

// SyntaxHighlight - renders a <pre><code> block (Go compiler handles chroma highlighting at build time).
export function SyntaxHighlight(props: { lang?: string; children?: any }): JSXNode {
  const lang = props.lang || "";
  const code = props.children != null ? String(props.children) : "";
  const cls = lang ? `chroma language-${lang}` : "chroma";
  return raw(`<pre class="chroma"><code class="${escapeHTML(cls)}">${escapeHTML(code)}</code></pre>`);
}
