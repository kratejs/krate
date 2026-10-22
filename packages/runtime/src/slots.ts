// Hydration helpers for compiler-emitted slot markers.
//
// Conditionals are emitted as comment markers around the active branch only:
//
//   <!--k:ID-->active branch<!--/k:ID-->
//
// with the inactive branch in a hoisted `<template data-kc="ID" data-kb="c|a">`
// (data-kb names the branch the template holds: c = consequent, a = alternate).
// The hydration bootstrap binds nodes inside template content too, so swapping
// moves the very nodes that were bound: handlers and nested bindings of both
// branches keep working across toggles.

interface CondState {
  /** Branch currently between the markers: 'c' or 'a'. */
  shown: 'c' | 'a';
  /** Detached nodes of the other branch. */
  other: DocumentFragment;
  end: Comment | null;
}

type CondStart = Comment & { __krateCond?: CondState };

function findCondEnd(start: Comment): Comment | null {
  const want = '/' + (start.nodeValue || '');
  for (let n = start.nextSibling; n; n = n.nextSibling) {
    if (n.nodeType === 8 && n.nodeValue === want) return n as Comment;
  }
  return null;
}

/**
 * Shows the consequent (`show` true) or alternate branch of the conditional
 * whose start marker is `start`. `tpl` is the conditional's hoisted template
 * (null when it is missing, in which case the other branch is empty and the
 * markers are assumed to hold the branch matching the first call).
 */
export function krateCondSwap(start: Comment, show: boolean, tpl: HTMLTemplateElement | null): void {
  const node = start as CondStart;
  const want: 'c' | 'a' = show ? 'c' : 'a';
  let st = node.__krateCond;
  if (!st) {
    const held = tpl ? tpl.getAttribute('data-kb') : null;
    const shown: 'c' | 'a' = held === 'c' ? 'a' : held === 'a' ? 'c' : want;
    const other = document.createDocumentFragment();
    if (tpl) other.appendChild(tpl.content);
    st = { shown, other, end: findCondEnd(start) };
    node.__krateCond = st;
  }
  if (st.shown === want) return;
  const parent = start.parentNode;
  if (!parent) return;
  if (!st.end || st.end.parentNode !== parent) st.end = findCondEnd(start);
  const end = st.end;
  const out = document.createDocumentFragment();
  let n = start.nextSibling;
  while (n && n !== end) {
    const next = n.nextSibling;
    out.appendChild(n);
    n = next;
  }
  parent.insertBefore(st.other, end);
  st.other = out;
  st.shown = want;
}

const URL_ATTRS: Record<string, true> = { href: true, src: true, action: true, formaction: true, 'xlink:href': true };

// Matches React 19's isJavaScriptProtocol: leading C0 controls/spaces, and
// tab/CR/LF anywhere inside the scheme, case-insensitively.
// eslint-disable-next-line no-control-regex
const JS_URL = /^[\u0000-\u001F ]*j[\r\n\t]*a[\r\n\t]*v[\r\n\t]*a[\r\n\t]*s[\r\n\t]*c[\r\n\t]*r[\r\n\t]*i[\r\n\t]*p[\r\n\t]*t[\r\n\t]*:/i;

/** The replacement React 19 (and the Krate compiler) uses for javascript: URLs. */
export const BLOCKED_URL = "javascript:throw new Error('React has blocked a javascript: URL as a security precaution.')";

/** Neutralizes a javascript: URL written to a URL-valued attribute. */
export function krateSanitizeAttr(attr: string, value: string): string {
  if (URL_ATTRS[attr.toLowerCase()] && JS_URL.test(value)) return BLOCKED_URL;
  return value;
}
