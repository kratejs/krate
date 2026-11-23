/// <reference types="react" />

/**
 * Opt-in typing bridge for apps that keep React-typed code (@types/react).
 *
 * Krate's global `JSX.Element` extends the DOM `Node`, which React's
 * `ReactNode` rejects. That breaks two setups:
 *
 * - `jsx: "react-jsx"`: the globals `<Link>`, `<Head>`, ... fail with TS2786.
 * - `jsx: "preserve"` + `jsxImportSource: "@krate/runtime"` (what
 *   `krate migrate nextjs` writes): every intrinsic element is a Krate
 *   `Element`, which React-typed props (`children: ReactNode`) reject.
 *
 * Importing this file makes `JSX.Element` also a `React.ReactElement`, so it is
 * assignable to `ReactNode` in both setups. Include it once, for example:
 *
 *   // src/krate-react.d.ts
 *   import "@krate/runtime/react-types";
 *
 * It needs @types/react; plain Krate apps should not import it.
 */
declare global {
  namespace JSX {
    // eslint-disable-next-line @typescript-eslint/no-empty-object-type
    interface Element extends React.ReactElement<any, any> {}
  }
}

export {};
