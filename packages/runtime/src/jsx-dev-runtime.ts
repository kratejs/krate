import { jsx } from './jsx-runtime.js';
import { type Component } from './dom.js';
export { jsx, jsxs, Fragment } from './jsx-runtime.js';
export type { Component } from './dom.js';

export function jsxDEV<P extends {}>(
  type: string | Component<P>,
  props: P | null,
  key?: string | null,
  _isStaticChildren?: boolean,
  _source?: { fileName: string; lineNumber: number; columnNumber: number },
  _self?: unknown
): Node {
  return jsx(type, props, key);
}
