// Minimal stand-in for the parts of @types/react 19 that matter to the
// Element/ReactNode relationship. Used by react-types.test.ts so the test does
// not need @types/react installed; set KRATE_REACT_TYPES to also run against the real package.
export = React;
export as namespace React;

declare namespace React {
  interface ReactElement<P = any, T = any> {
    type: T;
    props: P;
    key: string | null;
  }
  type ReactNode =
    | ReactElement
    | string
    | number
    | bigint
    | Iterable<ReactNode>
    | boolean
    | null
    | undefined
    | Promise<ReactNode>;
  type JSXElementConstructor<P> = (props: P) => ReactNode | Promise<ReactNode>;
  namespace JSX {
    interface Element extends ReactElement<any, any> {}
    type ElementType = string | JSXElementConstructor<any>;
    interface ElementChildrenAttribute { children: {} }
    interface IntrinsicAttributes { key?: string | number | null }
    interface IntrinsicElements { [tag: string]: any }
  }
}
