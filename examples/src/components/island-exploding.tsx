export function Exploding() {
  if (typeof window !== 'undefined' && (globalThis as any).__KRATE_TEST_ISLAND_ERROR) throw new Error('island explosion');
  return null;
}
