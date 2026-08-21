import { h, type Component } from './dom.js';
import './jsx-types.js';

export { type Component } from './dom.js';

export function Fragment(props: { children?: unknown[] }): DocumentFragment {
  const frag = document.createDocumentFragment();
  const kids = props.children;
  if (kids) {
    const list = Array.isArray(kids) ? kids : [kids];
    for (const child of list) {
      if (child instanceof Node) frag.appendChild(child);
      else frag.appendChild(document.createTextNode(String(child)));
    }
  }
  return frag;
}

export function jsx<P extends {}>(
  tag: string | Component<P>,
  props: P | null,
  _key?: string | null
): Node {
  const render = tag as string | ((props: Record<string, unknown>) => Node);
  if (!props) return h(render, null);
  const { children, ...rest } = props as unknown as Record<string, unknown>;
  const kids = children !== undefined
    ? (Array.isArray(children) ? children : [children])
    : [];
  return h(render, Object.keys(rest).length > 0 ? rest : null, ...kids);
}

export function jsxs<P extends {}>(
  tag: string | Component<P>,
  props: P | null,
  key?: string | null
): Node {
  return jsx(tag, props, key);
}


