// Krate lowers React's useId() at build time to a per-instance id that the
// static HTML and the hydration code share. Only the hook the components use
// is declared, so the package type-checks without @types/react.
declare module 'react' {
  export function useId(): string;
}
