// @krate/runtime/server - Server-side rendering runtime
// Provides the same API as the client runtime but renders to HTML strings.

export { getActionContext, headers, cookies, redirect } from './action-context.js';
export type { ActionContext, ActionCookies, ActionCookieOptions } from './action-context.js';
export { getRequestContext, notFound } from './request-context.js';
export type { RequestContext, RequestCookies } from './request-context.js';

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

function isThenable(v: unknown): v is PromiseLike<unknown> {
  return v != null && (typeof v === "object" || typeof v === "function") && typeof (v as any).then === "function";
}

function componentName(fn: Function): string {
  return (fn as any).displayName || fn.name || "anonymous";
}

function asyncInSyncRender(where: string): Error {
  return new Error(
    `${where} returned a Promise during a synchronous renderToString. Async server components ` +
      `are rendered per request by the SSR sidecar through renderToStringAsync; a synchronous ` +
      `render (for example inside goComponent children) cannot await them.`,
  );
}

function renderNode(node: JSXNode): string {
  if (node == null || node === false) return "";
  if (isThenable(node)) throw asyncInSyncRender("A child value");
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

/**
 * renderToStringAsync renders a tree that may contain async (server)
 * components. Every Promise-returning component is awaited, at any depth, and
 * sibling subtrees resolve in parallel. Errors thrown by a component (or a
 * rejected Promise) reject the returned Promise; nothing renders as "".
 *
 * <Suspense> renders its resolved children: the whole tree is awaited before
 * any HTML is produced, so there is no fallback to show.
 */
export async function renderToStringAsync(node: unknown): Promise<string> {
  const resolved = await resolveTree(node);
  return renderNode(resolved);
}

function isElement(v: unknown): v is JSXElement {
  return v != null && typeof v === "object" && "type" in (v as any) && "props" in (v as any) && Array.isArray((v as any).children);
}

function isJSXValue(v: unknown): boolean {
  return isElement(v) || isThenable(v) || (Array.isArray(v) && v.some(isJSXValue));
}

// resolveTree expands function components top-down into a tree of host
// elements, raw HTML and primitives, awaiting async components. Built-ins that
// render their children eagerly (marked __krateEagerChildren, e.g. Link) get
// their children and JSX-valued props resolved first, so an async component
// passed as a child still renders.
async function resolveTree(node: unknown): Promise<JSXNode> {
  if (isThenable(node)) return resolveTree(await node);
  if (node == null || typeof node !== "object") return node as JSXNode;
  if (Array.isArray(node)) return Promise.all(node.map(resolveTree));
  if ("__raw" in (node as any)) return node as RawHTML;
  if (!isElement(node)) return node as JSXNode;
  const { type, props, children } = node;
  if (typeof type !== "function") {
    return { type, props, children: (await Promise.all(children.map(resolveTree))) as JSXNode[] };
  }
  const fn = type as any;
  if (fn.__krateSuspense) return resolveTree(children);
  let callProps: Record<string, any> = { ...props, children };
  if (fn.__krateEagerChildren) {
    const entries = await Promise.all(
      Object.entries(props).map(async ([k, v]) => [k, isJSXValue(v) ? await resolveTree(v) : v] as const),
    );
    callProps = { ...Object.fromEntries(entries), children: await resolveTree(children) };
  }
  let out: unknown = fn(callProps);
  if (isThenable(out)) out = await out;
  return resolveTree(out);
}

function renderElement(el: JSXElement): string {
  const { type, props, children } = el;

  // Component
  if (typeof type === "function") {
    const result = type({ ...props, children });
    if (isThenable(result)) throw asyncInSyncRender(`<${componentName(type)}>`);
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

(Suspense as any).__krateSuspense = true;

// Client boundaries ("use client" modules inside a server tree)

type ClientRender = (component: unknown, props: Record<string, unknown>, childrenHTML: string | null) => string;

function describeValue(v: unknown): string {
  if (typeof v === "function") return "a function";
  if (typeof v === "symbol") return "a symbol";
  if (typeof v === "bigint") return "a bigint";
  if (v && typeof v === "object") {
    const ctor = (Object.getPrototypeOf(v) as { constructor?: { name?: string } } | null)?.constructor?.name;
    return ctor ? `an instance of ${ctor}` : "a non-plain object";
  }
  return typeof v;
}

// serializeClientProp converts a prop crossing from the server to a client
// component into JSON-safe data. Server actions become references the client
// chunk turns back into callable proxies; anything else that JSON cannot carry
// faithfully throws with the offending prop path.
function serializeClientProp(v: unknown, path: string, component: string): unknown {
  if (v === null || typeof v === "string" || typeof v === "boolean") return v;
  if (typeof v === "number") {
    if (!Number.isFinite(v)) throw clientPropError(path, component, `the number ${v}`);
    return v;
  }
  if (v === undefined) return undefined;
  if (isServerAction(v)) return { __krateAction: (v as any).actionUrl };
  if (Array.isArray(v)) return v.map((x, i) => serializeClientProp(x, `${path}[${i}]`, component));
  if (typeof v === "object") {
    if (isElement(v) || "__raw" in (v as any)) {
      throw clientPropError(path, component, "JSX", "only `children` can carry server-rendered JSX into a client component");
    }
    const proto = Object.getPrototypeOf(v);
    if (proto !== Object.prototype && proto !== null) throw clientPropError(path, component, describeValue(v));
    const out: Record<string, unknown> = {};
    for (const [k, x] of Object.entries(v as Record<string, unknown>)) {
      const sv = serializeClientProp(x, `${path}.${k}`, component);
      if (sv !== undefined) out[k] = sv;
    }
    return out;
  }
  throw clientPropError(path, component, describeValue(v));
}

function clientPropError(path: string, component: string, what: string, hint?: string): Error {
  return new Error(
    `Prop \`${path}\` passed to client component <${component}> is ${what}, which cannot be sent from a server ` +
      `component to the client. ` +
      (hint || "Pass JSON-serializable data (strings, numbers, booleans, null, plain objects, arrays) or a server action; " +
        "move event handlers and class instances into the client component."),
  );
}

/**
 * clientBoundary wraps an export of a `"use client"` module for the server
 * bundle of a page that renders server components. The server renders the
 * component's initial HTML (render) into a <krate-island> marker carrying its
 * serialized props; the per-module client chunk at src hydrates it in the
 * browser. Children are rendered on the server and passed as HTML.
 */
export function clientBoundary(
  moduleId: string,
  exportName: string,
  src: string,
  impl: unknown,
  render: ClientRender,
): unknown {
  if (typeof impl !== "function") return impl;
  const name = (impl as any).displayName || (impl as any).name || exportName;
  const Boundary = async (props: Record<string, any>) => {
    const { children, ...rest } = props || {};
    const data: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(rest)) {
      if (k === "key") continue;
      const sv = serializeClientProp(v, k, name);
      if (sv !== undefined) data[k] = sv;
    }
    const hasChildren = children != null && !(Array.isArray(children) && children.length === 0);
    const childrenHTML = hasChildren ? await renderToStringAsync(children) : null;
    if (childrenHTML !== null) data.__krateChildren = childrenHTML;
    const html = render(impl, data, childrenHTML);
    return raw(
      `<krate-island style="display:contents" data-module="${escapeHTML(moduleId)}" data-export="${escapeHTML(exportName)}" ` +
        `data-src="${escapeHTML(src)}" data-props="${escapeHTML(JSON.stringify(data))}">${html}</krate-island>`,
    );
  };
  Object.defineProperty(Boundary, "name", { value: name });
  return Boundary;
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

(Link as any).__krateEagerChildren = true;

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
