// @krate/runtime/server - Server-side rendering runtime
// Provides the same API as the client runtime but renders to HTML strings.

import { ATTR_ALIASES } from './htmlspec-attributes.js';
import { withBase } from './base-path.js';
import { highlight as highlightCode, trimSpace } from './highlight.js';
import { sealBoundArgs } from './action-seal.js';
import { accessDeniedStatus, isNotFoundSignal, redirectTarget } from './request-context.js';
import { sha256Hex } from './sha256.js';
import { currentRenderScope, markErrorCaught } from './cache-scope.js';
import { encodeProp } from './flight-props.js';

export { getActionContext, headers, cookies, redirect } from './action-context.js';
export type { ActionContext, ActionCookies, ActionCookieOptions } from './action-context.js';
export { revalidatePath, revalidateTag, unstable_cache, unstable_noStore } from './cache.js';
export { after } from './after.js';
export { getPlatform, getCloudflareContext } from './platform.js';
export type { PlatformContext, PlatformTarget, PlatformExecutionContext, CloudflareContext, KrateCloudflareEnv } from './platform.js';
export type { AfterTask, KrateRequest, KrateRequestCookies, KrateNextURL } from './after.js';
export type { RevalidatePathType, UnstableCacheOptions } from './cache.js';
export { getRequestContext, notFound, forbidden, unauthorized } from './request-context.js';
export {
  usePathname,
  useSearchParams,
  useParams,
  useRouter,
  useSelectedLayoutSegment,
  useSelectedLayoutSegments,
  permanentRedirect,
} from './navigation-server.js';
export type { AppRouterInstance } from './navigation-server.js';
export type { RequestContext, RequestCookies } from './request-context.js';
export { resolveMetadata, renderMetadataTags, metadataHeadTags } from './metadata.js';
export { renderSitemapXML, renderRobotsTXT, renderManifestJSON } from './metadata-routes.js';
export type { MetadataRoute, SitemapEntry, RobotsFile, RobotsRule, ManifestFile } from './metadata-routes.js';
export { ImageResponse } from './og.js';
export type { ImageResponseOptions } from './og.js';
export type { Metadata, ResolvingMetadata, ResolvingViewport, Viewport, MetadataSegment, MetadataModule, MetadataProps } from './metadata.js';

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

// Reads a custom hook parameter that a call site passed as a derived getter.
export function krateParam<T>(value: T | (() => T)): T {
  return typeof value === "function" && derivedGetters.has(value) ? (value as () => T)() : (value as T);
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
  type: string | Function | object | symbol;
  props: Record<string, any>;
  children: JSXNode[];
}

export function Fragment(_props: { children?: JSXNode }): JSXNode {
  return _props.children ?? [];
}

export function jsx(type: string | Function | object | symbol, props: Record<string, any>, _key?: string): JSXElement {
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

function isClassComponent(fn: any): boolean {
  return typeof fn === "function" && fn.prototype && typeof fn.prototype.render === "function";
}

// React element types that are objects or symbols rather than functions. A
// package compiled for React (Radix's Slot is a forwardRef) hands these to a
// dynamic tag (`const Comp = asChild ? Slot.Root : "button"`), so the server
// renderer must run them as components instead of printing them as a tag name.
const R_ELEMENT = new Set([Symbol.for("react.element"), Symbol.for("react.transitional.element")]);
const R_FORWARD_REF = Symbol.for("react.forward_ref");
const R_MEMO = Symbol.for("react.memo");
const R_LAZY = Symbol.for("react.lazy");
const R_PROVIDER = Symbol.for("react.provider");
const R_CONTEXT = Symbol.for("react.context");
const R_CONSUMER = Symbol.for("react.consumer");
const R_FRAGMENT = Symbol.for("react.fragment");
const R_SUSPENSE = Symbol.for("react.suspense");

type ForeignContext = { _currentValue?: unknown; _context?: ForeignContext; Provider?: unknown; Consumer?: unknown };

const exoticCache = new WeakMap<object, Function>();

function describeType(type: unknown): string {
  if (typeof type === "symbol") return type.toString();
  if (type && typeof type === "object") {
    const t = type as any;
    const tag = typeof t.$$typeof === "symbol" ? t.$$typeof.toString() : undefined;
    return tag ? `an object (${tag})` : "an object";
  }
  return String(type);
}

function notRenderable(type: unknown): Error {
  return new Error(
    `Cannot render an element whose type is ${describeType(type)}. A server render can run strings, ` +
      `function and class components, and React forwardRef, memo, lazy and context types; ` +
      `pass a component or an HTML tag name instead.`,
  );
}

// isSlotType reports Radix's Slot (createSlot names it `<owner>.Slot`). Its
// own render clones React elements and cannot work on Krate's, so the server
// uses ServerSlot for it.
function isSlotType(type: any): boolean {
  return typeof type.displayName === "string" && /(^|\.)Slot$/.test(type.displayName);
}

function childElements(children: unknown[]): unknown[] {
  return children.filter((c) => c != null && c !== false && !(typeof c === "string" && c.trim() === ""));
}

function mergeSlotProps(slot: Record<string, any>, child: Record<string, any>): Record<string, any> {
  const out: Record<string, any> = { ...slot, ...child };
  for (const key of Object.keys(child)) {
    const sv = slot[key];
    const cv = child[key];
    if (/^on[A-Z]/.test(key) && typeof sv === "function" && typeof cv === "function") {
      out[key] = (...args: unknown[]) => {
        cv(...args);
        sv(...args);
      };
    } else if (key === "style" && sv && cv && typeof sv === "object" && typeof cv === "object") {
      out[key] = { ...sv, ...cv };
    } else if ((key === "className" || key === "class") && sv && cv) {
      out[key] = `${sv} ${cv}`;
    }
  }
  return out;
}

// ServerSlot is Radix's Slot for the server: it renders its single child
// element with the Slot's own props merged onto it (the child wins, class
// names join, handlers compose).
function ServerSlot(allProps: Record<string, any>): JSXNode {
  const { children, ref: _ref, ...slotProps } = allProps;
  const kids = childElements(Array.isArray(children) ? children.flat(Infinity) : children == null ? [] : [children]);
  const child = kids.length === 1 ? normalizeNode(kids[0]) : undefined;
  if (!isElement(child)) return kids as JSXNode;
  return { type: child.type, props: mergeSlotProps(slotProps, child.props), children: child.children };
}

// toReactElement converts a React element object (made by a package's own
// React) into Krate's element shape.
function isReactElement(v: unknown): v is { type: unknown; props: Record<string, any> } {
  return v != null && typeof v === "object" && R_ELEMENT.has((v as any).$$typeof);
}

function normalizeNode(node: unknown): unknown {
  if (!isReactElement(node)) return node;
  const { children, ref: _ref, ...rest } = node.props ?? {};
  const kids = Array.isArray(children) ? children.flat(Infinity) : children != null ? [children] : [];
  return { type: node.type, props: rest, children: kids };
}

function foreignContext(type: any): { ctx: ForeignContext; provider: boolean } {
  if (type.$$typeof === R_PROVIDER) return { ctx: type._context, provider: true };
  if (type.$$typeof === R_CONSUMER) return { ctx: type._context, provider: false };
  if (type._context) return { ctx: type._context, provider: false };
  return { ctx: type, provider: type.Provider === type };
}

// withContextValue renders under a foreign Provider: consumers below read the
// value while its children are resolved.
function scopedContextFn(ctx: ForeignContext, provider: boolean): Function {
  if (provider) {
    const Provider = (props: Record<string, any>) => props.children;
    (Provider as any).__krateForeignProvider = ctx;
    return Provider;
  }
  const Consumer = (props: Record<string, any>) => consumerChild(props)(ctx._currentValue);
  (Consumer as any).__krateForeignConsumer = ctx;
  return Consumer;
}

function consumerChild(props: Record<string, any>): (value: unknown) => unknown {
  const render = Array.isArray(props.children) && props.children.length === 1 ? props.children[0] : props.children;
  if (typeof render !== "function") throw new Error("A context Consumer needs a function as its child.");
  return render;
}

// ContextValues holds the foreign Provider values in scope for an async
// resolve. Sibling subtrees resolve in parallel, so the values travel with the
// walk instead of living on the context object.
type ContextValues = ReadonlyMap<ForeignContext, unknown>;

// exoticType resolves a non-string, non-function element type to something
// the renderer can run. A pending React.lazy throws its promise.
function exoticType(type: any): string | Function {
  if (typeof type === "string" || typeof type === "function") return type;
  if (type === R_FRAGMENT || type === R_SUSPENSE) return Fragment;
  if (type == null || typeof type !== "object") throw notRenderable(type);
  switch (type.$$typeof) {
    case R_MEMO:
      return exoticType(type.type);
    case R_LAZY:
      return exoticType(type._init(type._payload));
    case R_FORWARD_REF: {
      let fn = exoticCache.get(type);
      if (!fn) {
        fn = isSlotType(type) ? ServerSlot : (props: Record<string, any>) => type.render(props, props.ref ?? null);
        exoticCache.set(type, fn);
      }
      return fn;
    }
    case R_PROVIDER:
    case R_CONSUMER:
    case R_CONTEXT: {
      let fn = exoticCache.get(type);
      if (!fn) {
        const { ctx, provider } = foreignContext(type);
        fn = scopedContextFn(ctx, provider);
        exoticCache.set(type, fn);
      }
      return fn;
    }
  }
  throw notRenderable(type);
}

// runForeignProvider evaluates `run` with a foreign Provider's value in place.
function withProviderValue<T>(fn: Function, props: Record<string, any>, run: () => T): T {
  const ctx = (fn as any).__krateForeignProvider as ForeignContext | undefined;
  if (!ctx) return run();
  const prev = ctx._currentValue;
  ctx._currentValue = props.value;
  try {
    return run();
  } finally {
    ctx._currentValue = prev;
  }
}

function classFallback(type: any, instance: any, error: unknown): unknown {
  if (typeof type.getDerivedStateFromError !== "function") throw error;
  instance.state = { ...instance.state, ...type.getDerivedStateFromError(error) };
  try {
    if (typeof instance.componentDidCatch === "function") instance.componentDidCatch(error, { componentStack: "" });
  } catch {
    // A throwing componentDidCatch must not hide the original render error.
  }
  return instance.render();
}

/**
 * ServerErrorBoundary is the request-time form of a segment's error.tsx under
 * an async layout: it renders `children`, and when rendering them throws it
 * renders `<fallback error={{ name, message, digest }} />` in their place.
 * notFound(), forbidden(), unauthorized() and redirect() signals pass through.
 * The visitor sees a generic message and a digest; the real error is logged
 * with the same digest. The render's scope records the catch, so the host
 * answers 500 with no-store instead of a cacheable 200.
 */
export class ServerErrorBoundary {
  props: { fallback: any; children?: JSXNode };
  state: { error: unknown } = { error: undefined };
  constructor(props: { fallback: any; children?: JSXNode }) {
    this.props = props;
  }
  static getDerivedStateFromError(error: unknown) {
    if (isNotFoundSignal(error) || accessDeniedStatus(error) || redirectTarget(error)) throw error;
    return { error: describeBoundaryError(error) };
  }
  render(): JSXNode {
    const err = this.state.error as { name: string; message: string; digest: string } | undefined;
    if (!err) return this.props.children as JSXNode;
    return jsx(this.props.fallback, { error: err }) as JSXNode;
  }
}

function describeBoundaryError(error: unknown) {
  const e = error as any;
  const message = (e && e.message) || String(error);
  const stack = (e && typeof e.stack === "string" && e.stack) || "";
  const digest = sha256Hex(message + "\n" + stack).slice(0, 10);
  console.error(`[krate] Error caught by an error boundary (digest ${digest}):`, error);
  markErrorCaught();
  return { name: "Error", message: "Internal Server Error", digest };
}

function renderNode(node: JSXNode): string {
  if (node == null || node === false) return "";
  if (isThenable(node)) throw asyncInSyncRender("A child value");
  if (typeof node === "string") return escapeHTML(node);
  if (typeof node === "number") return String(node);
  if (typeof node === "boolean") return "";
  if (Array.isArray(node)) return node.map(renderNode).join("");
  if (typeof node === "object" && "__raw" in node) return (node as RawHTML).__raw;
  if (isReactElement(node)) return renderElement(normalizeNode(node) as JSXElement);
  if (typeof node === "object" && "type" in node) return renderElement(node as JSXElement);
  return "";
}

function isServerAction(v: any): v is { actionUrl: string } {
  return typeof v === "function" && typeof (v as any).actionUrl === "string";
}

const UNITLESS = new Set([
  "animation-iteration-count", "aspect-ratio", "border-image-outset", "border-image-slice",
  "border-image-width", "box-flex", "box-flex-group", "box-ordinal-group", "column-count",
  "columns", "flex", "flex-grow", "flex-positive", "flex-shrink", "flex-negative", "flex-order",
  "grid-area", "grid-row", "grid-row-end", "grid-row-span", "grid-row-start", "grid-column",
  "grid-column-end", "grid-column-span", "grid-column-start", "font-weight", "line-clamp",
  "line-height", "opacity", "order", "orphans", "scale", "tab-size", "widows", "z-index", "zoom",
  "fill-opacity", "flood-opacity", "stop-opacity", "stroke-dasharray", "stroke-dashoffset",
  "stroke-miterlimit", "stroke-opacity", "stroke-width",
]);

const URL_ATTRS = new Set(["href", "src", "action", "formaction", "xlink:href"]);
// eslint-disable-next-line no-control-regex
const JS_URL = /^[\u0000-\u001F ]*j[\r\n\t]*a[\r\n\t]*v[\r\n\t]*a[\r\n\t]*s[\r\n\t]*c[\r\n\t]*r[\r\n\t]*i[\r\n\t]*p[\r\n\t]*t[\r\n\t]*:/i;
const BLOCKED_URL = "javascript:throw new Error('React has blocked a javascript: URL as a security precaution.')";

function cssName(key: string): string {
  if (key.startsWith("--")) return key;
  const name = key.replace(/[A-Z]/g, (c) => "-" + c.toLowerCase());
  return name.startsWith("ms-") ? "-" + name : name;
}

function styleString(value: Record<string, unknown>): string {
  const out: string[] = [];
  for (const key of Object.keys(value)) {
    const v = value[key];
    if (v == null || typeof v === "boolean" || v === "") continue;
    const prop = cssName(key);
    const bare = prop.replace(/^-(webkit|moz|ms|o)-/, "");
    const text = typeof v === "number" && v !== 0 && !prop.startsWith("--") && !UNITLESS.has(bare)
      ? v + "px"
      : String(v);
    out.push(prop + ":" + text);
  }
  return out.join(";");
}

function setAttr(key: string, val: any): string {
  if (key === "children" || key === "key" || key === "ref" || key === "dangerouslySetInnerHTML") return "";
  const name = ATTR_ALIASES[key] || key;
  if (val == null || val === false) return "";
  if (val === true) return ` ${name}`;
  if (name === "action" && isServerAction(val)) {
    return ` action="${escapeHTML(val.actionUrl)}" data-krate-action`;
  }
  if (name === "formaction" && isServerAction(val)) {
    return ` formaction="${escapeHTML(val.actionUrl)}" formmethod="post" data-krate-action`;
  }
  // Client function actions run only after hydration; they have no HTML form.
  if (typeof val === "function") return "";
  if (key === "style" && typeof val === "object") {
    const css = styleString(val as Record<string, unknown>);
    return css ? ` style="${escapeHTML(css)}"` : "";
  }
  if (key.startsWith("on") || key.startsWith("__")) return "";
  let text = String(val);
  if (URL_ATTRS.has(name.toLowerCase()) && JS_URL.test(text)) text = BLOCKED_URL;
  return ` ${name}="${escapeHTML(text)}"`;
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
async function resolveTree(node: unknown, vals?: ContextValues): Promise<JSXNode> {
  if (isThenable(node)) return resolveTree(await node, vals);
  if (node == null || typeof node !== "object") return node as JSXNode;
  if (Array.isArray(node)) return Promise.all(node.map((n) => resolveTree(n, vals)));
  if ("__raw" in (node as any)) return node as RawHTML;
  if (isReactElement(node)) node = normalizeNode(node);
  if (!isElement(node)) return node as JSXNode;
  const { type, props, children } = node;
  if (typeof type === "string") {
    return { type, props, children: (await Promise.all(children.map((n) => resolveTree(n, vals)))) as JSXNode[] };
  }
  const step = await expandComponent(node, vals);
  return resolveTree(step.out, step.vals);
}

// expandComponent runs one component element (function, class, React exotic
// type, foreign Provider or Consumer) and returns what it rendered, with the
// context values in scope for that output.
async function expandComponent(node: JSXElement, vals?: ContextValues): Promise<{ out: unknown; vals?: ContextValues }> {
  const { type, props, children } = node;
  let fn: any = type;
  while (typeof fn !== "function") {
    try {
      fn = exoticType(fn);
    } catch (e) {
      if (!isThenable(e)) throw e;
      await e;
    }
  }
  if (fn.__krateSuspense) return { out: children, vals };
  let callProps: Record<string, any> = { ...props, children };
  if (fn.__krateEagerChildren) {
    const entries = await Promise.all(
      Object.entries(props).map(async ([k, v]) => [k, isJSXValue(v) ? await resolveTree(v, vals) : v] as const),
    );
    callProps = { ...Object.fromEntries(entries), children: await resolveTree(children, vals) };
  }
  if (isClassComponent(fn)) {
    const instance = new fn(callProps);
    try {
      return { out: await resolveTree(instance.render(), vals), vals };
    } catch (error) {
      return { out: classFallback(fn, instance, error), vals };
    }
  }
  if (fn.__krateForeignConsumer) {
    const ctx = fn.__krateForeignConsumer as ForeignContext;
    return { out: consumerChild(callProps)(vals?.has(ctx) ? vals.get(ctx) : ctx._currentValue), vals };
  }
  // A client boundary also gets the context values in scope (slotValues).
  let out: unknown = fn.__krateClient ? fn(callProps, vals, RENDER_CALL) : fn(callProps);
  if (isThenable(out)) out = await out;
  if (fn.__krateForeignProvider) {
    return { out, vals: new Map(vals).set(fn.__krateForeignProvider as ForeignContext, props.value) };
  }
  return { out, vals };
}

// rawTextContent renders the children of <script>/<style>: their text is raw
// text in HTML, so entity escaping would corrupt JSON-LD and inline code. The
// static compiler emits it the same way; only a sequence that could end the
// element or open a comment is neutralised.
function rawTextContent(tag: string, children: JSXNode[]): string {
  let out = "";
  for (const c of children) {
    if (typeof c === "string") out += c;
    else out += renderNode(c);
  }
  out = out.replace(tag === "script" ? /<\/(script)/gi : /<\/(style)/gi, "<\\/$1");
  return tag === "script" ? out.replace(/<!--/g, "<\\!--") : out;
}

function renderElement(el: JSXElement): string {
  const { type, props, children } = el;

  // Component
  if (typeof type !== "string" && typeof type !== "function") {
    let fn: any;
    try {
      fn = exoticType(type);
    } catch (e) {
      if (isThenable(e)) throw asyncInSyncRender("A lazy component");
      throw e;
    }
    return renderElement({ type: fn, props, children });
  }
  if (typeof type === "function") {
    if (isClassComponent(type)) {
      const instance = new (type as any)({ ...props, children });
      try {
        return renderNode(instance.render());
      } catch (error) {
        return renderNode(classFallback(type, instance, error) as JSXNode);
      }
    }
    const result = (type as any).__krateClient ? (type as any)({ ...props, children }, undefined, RENDER_CALL) : type({ ...props, children });
    if (isThenable(result)) throw asyncInSyncRender(`<${componentName(type)}>`);
    return withProviderValue(type, props, () => renderNode(result));
  }

  // HTML element
  const tag = type as string;
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

  const dangerous = props.dangerouslySetInnerHTML;
  const inner = dangerous && typeof dangerous === "object" && typeof dangerous.__html === "string"
    ? dangerous.__html
    : tag === "script" || tag === "style"
    ? rawTextContent(tag, children)
    : children.map(renderNode).join("");
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

/**
 * lazy() in a server tree: the loader's module is imported while the page
 * renders and its component renders in place. Nothing is code-split to the
 * browser, so a server-only module can be loaded this way.
 */
export function lazy<P = Record<string, any>>(
  loader: () => Promise<{ default: (props: P) => unknown } | ((props: P) => unknown)>,
): (props: P & { children?: JSXNode }) => Promise<JSXNode> {
  let loaded: Promise<unknown> | undefined;
  return async function Lazy(props) {
    loaded ??= Promise.resolve(loader());
    const mod: any = await loaded;
    const C = typeof mod === "function" ? mod : mod?.default;
    if (typeof C !== "function") {
      throw new Error("lazy(): the loader did not resolve to a component; export it as the module's default or return it from .then()");
    }
    return jsx(C, props as Record<string, any>);
  };
}

// Client boundaries ("use client" modules inside a server tree)

// fwd is the context forwarded into the slot the component renders in; the
// render records the values in scope at each of its own slots in capture, by
// slot token (see islandcontext.go in the compiler).
type ClientRender = (
  component: unknown,
  props: Record<string, unknown>,
  childrenHTML: string | null,
  fwd?: SlotValues,
  capture?: Record<string, SlotValues>,
  ids?: string,
) => string;

type SlotValues = { k: string; v: unknown }[];

// slotValues is the ContextValues entry holding the client context forwarded
// into a server-rendered slot of a client component: nested client
// components render inside it, as they would in one React tree.
const slotValues: ForeignContext = {};

// A deferred slot is server-rendered HTML a client component receives (its
// children, a JSX prop, a server component in a children tree). It renders
// after the component, inside the context the component provides there.
//
// The server render module leaves a slot's token (an open comment marker) in
// the component's HTML where the slot goes, with no element around it, so the
// children sit directly in the component's own element as they do in React.
// In an island the HTML follows the marker and ends at a close marker
// (slotClose); the browser finds a slot the component rendered by those
// markers and adopts its nodes in place, so its props carry only the id
// (done gets null). A slot the server did not render (closed dialog content,
// a real-React island's tree) travels as HTML (done gets it).
type DeferredSlot = { token: string; id: string; node: unknown; done: (html: string | null) => void; vals?: ContextValues };

let slotSeq = 0;

function deferSlot(ctx: FlightCtx, node: unknown, done: (html: string | null, id: string) => void): string {
  slotSeq = (slotSeq + 1) % 1e9;
  const id = slotSeq.toString(36) + Math.random().toString(36).slice(2, 8);
  const token = `<!--krate-slot:${id}-->`;
  ctx.slots.push({ token, id, node, done: (html) => done(html, id), vals: ctx.vals });
  return token;
}

// slotClose is the marker that ends the HTML of the slot with this id.
const slotClose = (id: string) => `<!--/krate-slot:${id}-->`;

// mergeSlotValues layers the values a component provides at a slot over the
// ones forwarded into the component.
function mergeSlotValues(outer: SlotValues | undefined, own: SlotValues | undefined): SlotValues | undefined {
  if (!own) return outer;
  if (!outer || outer.length === 0) return own;
  const keys = new Set(own.map((e) => e.k));
  return outer.filter((e) => !keys.has(e.k)).concat(own);
}

const flightIsAction = (v: unknown): string | undefined => (isServerAction(v) ? (v as any).actionUrl : undefined);
const flightIsJSX = (v: unknown): boolean => isElement(v) || "__raw" in (v as any);

// serializeClientProp converts a prop crossing from the server to a client
// component into JSON-safe data (see flight-props.ts for the encoding). Server
// actions become references the client chunk turns back into callable
// proxies; anything JSON and the tag encoding cannot carry faithfully throws
// with the offending prop path.
function serializeClientProp(v: unknown, path: string, component: string): unknown {
  if (v === undefined) return undefined;
  return encodeProp(v, path, {
    isAction: flightIsAction,
    sealBound: sealBoundArgs,
    isJSX: flightIsJSX,
    fail: (p, what, hint) => clientPropError(p, component, what, hint),
  });
}

function clientPropError(path: string, component: string, what: string, hint?: string): Error {
  return new Error(
    `Prop \`${path}\` passed to client component <${component}> is ${what}, which cannot be sent from a server ` +
      `component to the client. ` +
      (hint || "Pass serializable data (strings, numbers, booleans, null, undefined, bigint, Date, Map, Set, typed arrays, " +
        "plain objects, arrays) or a server action; move event handlers and class instances into the client component."),
  );
}

type ClientRef = { moduleId: string; exportName: string; src: string; impl: unknown; hydrate: boolean };

function clientRegistry(): Map<string, ClientRef> {
  const g = globalThis as any;
  return (g.__krateClientImpls ??= new Map<string, ClientRef>());
}

function clientRefOf(type: unknown): ClientRef | undefined {
  return typeof type === "function" ? (type as any).__krateClient : undefined;
}

// hasClientElement reports whether a children value holds a client boundary
// element reachable through host elements and arrays. Server components stop
// the walk: they render to HTML on their own.
function hasClientElement(node: unknown): boolean {
  if (node == null || typeof node !== "object") return false;
  if (Array.isArray(node)) return node.some(hasClientElement);
  if (!isElement(node)) return false;
  if (clientRefOf(node.type)) return true;
  return typeof node.type === "string" && node.children.some(hasClientElement);
}

// vals is set when server components are run while building the tree (see
// toFlight), so their output and deferred slots keep the server context.
type FlightCtx = { hydrate: boolean; component: string; slots: DeferredSlot[]; vals?: ContextValues };

// isSlotValue reports whether a prop holds server-rendered JSX (an element, raw
// HTML, or an array containing either). Such a prop is a slot: it is rendered
// on the server and revived on the client like `children`.
function isSlotValue(v: unknown): boolean {
  if (Array.isArray(v)) return v.some(isSlotValue);
  return v != null && typeof v === "object" && (isElement(v) || "__raw" in (v as any));
}

// slotProp renders a JSX-valued prop. A slot without client components of its
// own travels as HTML; one with them (or any slot when tree is set) travels as
// a tree that keeps those components in the island's Preact tree.
async function slotProp(v: unknown, ctx: FlightCtx, tree: boolean): Promise<unknown> {
  if (tree || hasClientElement(v)) {
    const nested: FlightCtx = { hydrate: false, component: ctx.component, slots: ctx.slots, vals: ctx.vals };
    const out = await toFlight(v, nested);
    if (nested.hydrate) ctx.hydrate = true;
    return { __krateSlotTree: out };
  }
  const slot: Record<string, string> = { __krateSlotHTML: "" };
  slot.__krateSlotHTML = deferSlot(ctx, v, (html, id) => {
    if (html !== null) slot.__krateSlotHTML = html;
    else {
      delete slot.__krateSlotHTML;
      slot.__krateSlotRef = id;
    }
  });
  return slot;
}

// clientProps serializes the props of a client component, rendering the
// JSX-valued ones (any name, not only `children`) as slots.
async function clientProps(props: Record<string, unknown>, ctx: FlightCtx, tree: boolean): Promise<Record<string, unknown>> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(props)) {
    if (k === "key" || k === "children") continue;
    const sv = isSlotValue(v) ? await slotProp(v, ctx, tree) : serializeClientProp(v, k, ctx.component);
    if (sv !== undefined) out[k] = sv;
  }
  return out;
}

// toFlight turns the children of a client boundary into a serializable tree
// that keeps nested client components as components, so they render inside the
// parent's Preact tree and share its context (Radix Dialog.Root with its
// Trigger and Content). Everything else is host markup or server-rendered HTML.
//
// With inline set (the children of an asChild component) a server component
// at the top of a child is run here instead of becoming HTML, until a host
// element or client component comes out: Radix's Slot clones that element and
// merges its handlers, ref and ARIA props into it, as it does with the React
// element RSC hands it. Client components in its output stay in the tree.
async function toFlight(node: unknown, ctx: FlightCtx, inline = false): Promise<unknown[]> {
  if (isThenable(node)) return toFlight(await node, ctx, inline);
  if (node == null || typeof node === "boolean") return [];
  if (typeof node === "string" || typeof node === "number") return [node];
  if (Array.isArray(node)) {
    const out: unknown[] = [];
    for (const n of node) out.push(...(await toFlight(n, ctx, inline)));
    return out;
  }
  if (typeof node !== "object") return [];
  if ("__raw" in (node as any)) return [htmlSlot(ctx, node)];
  if (inline && isReactElement(node)) node = normalizeNode(node);
  if (!isElement(node)) return [];
  const { type, props, children } = node;
  if (typeof type === "string") {
    return [{ $: "e", t: type, p: await clientProps(props, ctx, true), c: await toFlight(children, ctx) }];
  }
  if (type === Link) {
    return [{ $: "e", t: "a", p: linkAttributes({ ...props, children }), c: await toFlight(children, ctx) }];
  }
  const ref = clientRefOf(type);
  if (ref) {
    if (ref.hydrate) ctx.hydrate = true;
    const nested = { ...ctx, component: ref.exportName };
    const c = await toFlight(children, nested, !!props.asChild);
    if (nested.hydrate) ctx.hydrate = true;
    if (props.asChild) warnUncloneable(ref.exportName, c);
    return [{ $: "c", m: ref.moduleId, e: ref.exportName, s: ref.src, p: await clientProps(props, nested, true), c }];
  }
  if (inline) {
    const step = await expandComponent(node, ctx.vals);
    const nested: FlightCtx = { ...ctx, vals: step.vals };
    const out = await toFlight(step.out, nested, true);
    if (nested.hydrate) ctx.hydrate = true;
    return out;
  }
  return [htmlSlot(ctx, node)];
}

// htmlSlot is a server-rendered part of a children tree: { $: "h", h: html },
// or { $: "h", r: id } once the component has rendered it into the document.
function htmlSlot(ctx: FlightCtx, node: unknown): Record<string, string> {
  const slot: Record<string, string> = { $: "h", h: "" };
  slot.h = deferSlot(ctx, node, (html, id) => {
    if (html !== null) slot.h = html;
    else {
      delete slot.h;
      slot.r = id;
    }
  });
  return slot;
}

// propsScript is the JSON script that carries a real-React island's props,
// placed just before its <krate-island>. Every "<" is escaped, so no
// "</script" or "<!--" in a value can end the script or open a comment.
function propsScript(data: unknown): string {
  return `<script type="application/json" data-krate-props>${JSON.stringify(data).replace(/</g, "\\u003c")}</script>`;
}

// ISLAND_OPEN starts the comment that opens a Preact island; ISLAND_CLOSE is
// the comment that ends it. The island's HTML sits between them with no
// element of its own, so child combinators and sibling selectors of the
// parent (space-y-*, divide-*, > :first-child) see the component's root
// nodes, as in React.
export const ISLAND_OPEN = "krate-island:";
export const ISLAND_CLOSE = "<!--/krate-island-->";

// islandOpen is the opening comment of a Preact island: its module, export,
// chunk and props as JSON. "<", ">" and "--" are escaped (they occur only in
// strings), so no value can end the comment or open another one.
function islandOpen(moduleId: string, exportName: string, src: string, props: unknown, ids: string): string {
  const json = JSON.stringify({ m: moduleId, e: exportName, s: src, i: ids, p: props })
    .replace(/</g, "\\u003c")
    .replace(/>/g, "\\u003e")
    .replace(/--/g, "-\\u002d");
  return `<!--${ISLAND_OPEN}${json}-->`;
}

// islandIdPrefix names the next island of a component in the render that is
// running: a hash of its module and export, and how many islands of that
// component came before it in this document. useId() numbers from 0 inside
// each island, so the prefix is what keeps ids unique across the page. The
// router pairs the n-th island of a component in the live page with the n-th
// one in the new markup, so a page that a navigation swaps in never reuses the
// prefix of an island the swap keeps.
const looseIslands = new Map<string, number>();

function islandIdPrefix(moduleId: string, exportName: string): string {
  const key = moduleId + "#" + exportName;
  const scope = currentRenderScope();
  const seen = scope ? (scope.islands ??= new Map<string, number>()) : looseIslands;
  const n = seen.get(key) ?? 0;
  seen.set(key, n + 1);
  let h = 0x811C9DC5;
  for (let i = 0; i < key.length; i++) h = Math.imul(h ^ key.charCodeAt(i), 0x01000193) >>> 0;
  return h.toString(36).padStart(7, "0") + "_" + n;
}

const warnedUncloneable = new Set<string>();

// RENDER_CALL is the third argument the renderer passes when it renders a
// client boundary as a component (a registered symbol: the renderer bundles its own
// copy of this module). A call without it is user code calling an
// export of a "use client" module as a function.
const RENDER_CALL = Symbol.for("krate.render");

function clientCallError(exportName: string, name: string): Error {
  const what = exportName === "default" ? name : exportName;
  return new Error(
    `Attempted to call ${what}() from the server but ${what} is on the client. ` +
      `It is exported by a "use client" module, so a server component can render it as <${what} /> ` +
      `or pass it to a client component, but cannot call it. ` +
      `Move the function into a module without "use client" and import it from both sides.`,
  );
}

// warnUncloneable reports an asChild component whose child reached the
// client as server-rendered HTML (raw HTML, a deferred slot), which its Slot
// cannot clone or merge props into.
function warnUncloneable(component: string, tree: unknown[]): void {
  if (!tree.some((n) => n != null && typeof n === "object" && (n as any).$ === "h")) return;
  if (warnedUncloneable.has(component)) return;
  warnedUncloneable.add(component);
  console.warn(
    `[krate] <${component} asChild> received a child rendered to HTML, so it cannot merge its props and handlers ` +
      `into it. Pass a single element or a component that renders one (not raw HTML) as the child.`,
  );
}

function wrapClientNamespace(
  moduleId: string,
  exportName: string,
  src: string,
  ns: Record<string, unknown>,
  render: ClientRender,
  hydrate: boolean,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const key of Object.keys(ns)) {
    const v = ns[key];
    const isComponentName = /^[A-Z]/.test(key);
    if (isComponentName && typeof v === "function") {
      out[key] = clientBoundary(moduleId, `${exportName}.${key}`, src, v, render, hydrate);
    } else if (isComponentName && v && typeof v === "object" && !Array.isArray(v)) {
      out[key] = wrapClientNamespace(moduleId, `${exportName}.${key}`, src, v as Record<string, unknown>, render, hydrate);
    } else {
      out[key] = v;
    }
  }
  return out;
}

/**
 * clientBoundary wraps an export of a `"use client"` module, or a component
 * of a React package, for the server bundle of a page that renders server
 * components. The server renders the component's initial HTML (render) and,
 * when it hydrates, puts it between <!--krate-island:{...}--> (module,
 * export, chunk and serialized props) and <!--/krate-island--> with no
 * element around it; the per-module client chunk at src hydrates those nodes
 * in the browser. A real-React island (flight) gets a <krate-island> element
 * instead, preceded by a JSON script with its props, because createRoot needs
 * a container. A component without state or effects (hydrate
 * false, an icon) is plain HTML. Children are rendered on the server into the
 * component's HTML and adopted there by the browser (see DeferredSlot), or,
 * when they hold client components of their own, sent as a tree that keeps
 * those components in the same Preact tree. The children of an asChild component
 * always travel as a tree, with a server component at the top of a child run
 * to the element it renders, so the component's Slot can clone it. A
 * namespace export (Radix's Dialog) is wrapped member by member. With `flight` the children always travel as a
 * tree (a real-React island rebuilds them as elements, not HTML).
 */
export function clientBoundary(
  moduleId: string,
  exportName: string,
  src: string,
  impl: unknown,
  render: ClientRender,
  hydrate = true,
  flight = false,
): unknown {
  if (impl && typeof impl === "object" && !Array.isArray(impl)) {
    return wrapClientNamespace(moduleId, exportName, src, impl as Record<string, unknown>, render, hydrate);
  }
  if (typeof impl !== "function") return impl;
  const name = (impl as any).displayName || (impl as any).name || exportName;
  // Called by user code, the boundary fails here and now. An async function
  // would hand back a rejected promise the caller never awaits, and that
  // unhandled rejection would take the renderer process down.
  const Boundary = (props: Record<string, any>, vals?: ContextValues, from?: symbol): Promise<unknown> => {
    if (from !== RENDER_CALL) throw clientCallError(exportName, name);
    return renderBoundary(props, vals);
  };
  const renderBoundary = async (props: Record<string, any>, vals?: ContextValues) => {
    // Taken before the first await, so islands number in render order.
    const ids = islandIdPrefix(moduleId, exportName);
    const { children, ...rest } = props || {};
    const slots: DeferredSlot[] = [];
    const slotCtx: FlightCtx = { hydrate: false, component: name, slots, vals };
    const data = await clientProps(rest, slotCtx, flight);
    const hasChildren = children != null && !(Array.isArray(children) && children.length === 0);
    let hydrates = hydrate || slotCtx.hydrate;
    // An asChild component clones its child, so the child travels as a tree.
    const asChild = !!rest.asChild;
    if (hasChildren && (flight || asChild || hasClientElement(children))) {
      const ctx: FlightCtx = { hydrate: false, component: exportName, slots, vals };
      data.__krateTree = await toFlight(children, ctx, asChild);
      if (ctx.hydrate) hydrates = true;
      if (asChild) warnUncloneable(exportName, data.__krateTree as unknown[]);
    } else if (hasChildren) {
      data.__krateChildren = deferSlot(slotCtx, children, (html, id) => (data.__krateChildren = html ?? { __krateSlotRef: id }));
    }
    const fwd = vals?.get(slotValues) as SlotValues | undefined;
    const capture: Record<string, SlotValues> = {};
    let html = render(impl, data, null, fwd, capture, ids);
    const rendered = await Promise.all(
      slots.map(async (s) => {
        const scope = mergeSlotValues(fwd, capture[s.token]);
        const base = s.vals ?? vals;
        const inner = scope ? new Map(base).set(slotValues, scope) : base;
        return renderNode(await resolveTree(s.node, inner));
      }),
    );
    // A slot whose token is in the render is in the document: the browser
    // adopts those nodes, so they are not sent again inside the props. An
    // island keeps the token as the slot's open marker and closes it after
    // the HTML; a component without an island needs no markers.
    const placed = slots.map((s) => html.includes(s.token));
    slots.forEach((s, i) => {
      s.done(placed[i] ? null : rendered[i]);
      html = html.split(s.token).join(hydrates ? s.token + rendered[i] + slotClose(s.id) : rendered[i]);
    });
    if (!hydrates) return raw(html);
    // A real-React island (flight) mounts with createRoot, which needs an
    // element of its own; a Preact island hydrates the nodes between its two
    // comments in place.
    if (!flight) return raw(islandOpen(moduleId, exportName, src, data, ids) + html + ISLAND_CLOSE);
    return raw(
      propsScript(data) +
        `<krate-island style="display:contents" data-module="${escapeHTML(moduleId)}" data-export="${escapeHTML(exportName)}" ` +
        `data-src="${escapeHTML(src)}">${html}</krate-island>`,
    );
  };
  Object.defineProperty(Boundary, "name", { value: name });
  const ref: ClientRef = { moduleId, exportName, src, impl, hydrate };
  (Boundary as any).__krateClient = ref;
  clientRegistry().set(`${moduleId}#${exportName}`, ref);
  return Boundary;
}

// Built-in Components (server stubs)

// Head - renders children into the page head. At build time the compiler
// hoists them. In a request-time render the children come out between two
// comment markers, and the host (ssr-core takeHead) moves what sits between
// them into the document <head> of the response instead of leaving it in the
// body.
export const HEAD_OPEN = "<!--krate-head-->";
export const HEAD_CLOSE = "<!--/krate-head-->";
export function Head(props: { children?: JSXNode }): JSXNode {
  return [raw(HEAD_OPEN), props.children ?? "", raw(HEAD_CLOSE)];
}

// Script and Style render the element in place with every attribute (src,
// defer, async, data-*, nonce, ...), as the static compiler does. Their
// children are raw text. `strategy` is next/script's and has no HTML meaning.
export function Script(props: Record<string, any>): JSXNode {
  const { strategy: _strategy, ...rest } = props;
  return jsx("script", rest);
}

export function Style(props: Record<string, any>): JSXNode {
  return jsx("style", props);
}

// linkAttributes computes the attributes of a <Link> anchor. An empty string
// stands for a boolean attribute.
function linkAttributes(props: Record<string, any>): Record<string, string> {
  const { href = "", prefetch, replace, scroll, basePath: noBase, children: _children, ...rest } = props;
  const attrs: Record<string, string> = {};
  // basePath={false}: the URL belongs to another app on the host.
  const outside = noBase === false;
  if (href) attrs.href = outside ? href : withBase(href);

  const external =
    outside ||
    /^(https?:|mailto:|tel:|#|javascript:)/.test(href) ||
    href.startsWith("//") ||
    rest.target === "_blank" ||
    rest.download !== undefined;

  if (external) {
    attrs["data-krate-external"] = "";
    if (outside) attrs["data-krate-no-base"] = "";
  } else {
    attrs["data-krate-link"] = "";
    if (prefetch !== false) attrs["data-prefetch"] = "";
    if (replace) attrs["data-krate-replace"] = "";
    if (scroll === false) attrs["data-krate-scroll"] = "false";
  }

  for (const key of ["target", "rel", "title", "id", "aria-label"]) {
    const v = rest[key];
    if (v !== undefined && v !== null) attrs[key] = String(v);
  }
  if (rest.target === "_blank" && rest.rel === undefined) attrs.rel = "noopener noreferrer";
  if (rest.className) attrs.class = String(rest.className);
  // Every other attribute reaches the anchor, so data-*, aria-*, class and
  // style that a Slot (asChild) merges onto a Link are not lost.
  for (const [key, v] of Object.entries(rest)) {
    const name = ATTR_ALIASES[key] || key;
    if (name in attrs || v == null || v === false || typeof v === "function") continue;
    if (key === "key" || key === "ref" || key === "className" || key === "dangerouslySetInnerHTML" || key.startsWith("on") || key.startsWith("__")) continue;
    if (key === "style" && typeof v === "object") {
      const css = styleString(v as Record<string, unknown>);
      if (css) attrs.style = css;
      continue;
    }
    attrs[name] = v === true ? "" : String(v);
  }
  return attrs;
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
  const attrs = Object.entries(linkAttributes(props)).map(([k, v]) =>
    v === "" ? k : `${k}="${escapeHTML(v)}"`,
  );
  const inner = props.children != null ? renderNode(props.children) : "";
  return raw(`<a ${attrs.join(" ")}>${inner}</a>`);
}

(Link as any).__krateEagerChildren = true;

// Image - renders an <img> tag (server stub; Go compiler handles full responsive output at build time).
export function Image(props: { src?: string; alt?: string; width?: number; height?: number; [key: string]: any }): JSXNode {
  const { src = "", alt = "", width, height } = props;
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

// SyntaxHighlight - renders a <pre><code> block. The Go compiler highlights code it knows at build
// time; code that only exists at request time is highlighted here with the same lexers (see
// highlight.ts), when the language's tables are loaded and the text is within its limits, and is
// written as plain text otherwise.
export function SyntaxHighlight(props: { lang?: string; highlight?: string; children?: any }): JSXNode {
  const lang = props.lang || "";
  const code = props.children != null ? String(props.children) : "";
  const lines = props.highlight || "";
  const html = lang ? highlightCode(trimSpace(code), lang, lines || undefined) : null;
  if (html !== null) {
    const attr = lines ? ` data-highlight-lines="${escapeHTML(lines)}"` : "";
    return raw(`<pre class="chroma"${attr}><code class="language-${escapeHTML(lang)}">${html}</code></pre>`);
  }
  const cls = lang ? `chroma language-${lang}` : "chroma";
  return raw(`<pre class="chroma"><code class="${escapeHTML(cls)}">${escapeHTML(code)}</code></pre>`);
}
