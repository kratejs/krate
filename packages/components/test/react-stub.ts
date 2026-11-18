// Stand-in for React's useId in unit tests; Krate's compiler resolves the real
// call at build time.
let n = 0;
export function useId(): string {
  return 'test-id-' + n++;
}
