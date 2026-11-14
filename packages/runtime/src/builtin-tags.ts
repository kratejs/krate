// Importable values for the tags the compiler provides (<Head>, <Link>, ...).
//
// A page or component normally uses them as globals and the compiler lowers
// them: <Head> children are hoisted into the document head, <Link> becomes an
// anchor wired for navigation. These values exist so that
// `import { Head } from "@krate/runtime"` resolves to a real export in every
// bundle that links the runtime (React islands, tooling). Request-time server
// bundles map the same names to the server renderer in @krate/runtime/server,
// so these client-side values never decide what the server writes.

import { h } from "./dom.js";

type Props = Record<string, unknown> & { children?: unknown };

/** Head content is hoisted into the document at build time; nothing renders in place. */
function headTag(_props: Props): null {
  return null;
}

function iconTag(_props: Props): null {
  return null;
}

function syntaxHighlightTag(props: Props): unknown {
  return h("pre", null, h("code", null, props.children));
}

function suspenseTag(props: Props): unknown {
  return props.children ?? null;
}

function linkTag(props: Props): unknown {
  const { children, prefetch: _prefetch, replace: _replace, scroll: _scroll, ...rest } = props;
  return h("a", rest, children);
}

// The typed tags keep the prop types of their global declarations.
export const Head = headTag as unknown as typeof globalThis.Head;
export const Icon = iconTag as unknown as typeof globalThis.Icon;
export const SyntaxHighlight = syntaxHighlightTag as unknown as typeof globalThis.SyntaxHighlight;
export const Suspense = suspenseTag as unknown as typeof globalThis.Suspense;
export const Link = linkTag as unknown as typeof globalThis.Link;

export function Script(_props: Props): null {
  return null;
}

export function Style(_props: Props): null {
  return null;
}

export function Image(props: Props): unknown {
  return h("img", props);
}
