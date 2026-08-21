// @krate/runtime/server/jsx-runtime — Server-compatible JSX runtime.
// The functions are re-exported from ./server.js (single source of truth) so
// the JSX transform and its types never drift between the two server subpaths.

type JSXNode = string | number | boolean | null | undefined | JSXNode[] | JSXElement;

interface JSXElement {
  type: string | Function;
  props: Record<string, any>;
  children: JSXNode[];
}

export { Fragment, jsx, jsxs } from './server.js';
