import { isDerivedGetter } from './signal.js';

/**
 * Reads a custom hook parameter. A call site passes an argument that reads
 * reactive state as a derived getter (the compiler wraps it), so the hook
 * sees the current value wherever the parameter is read; any other argument
 * is returned as is.
 */
export function krateParam<T>(value: T | (() => T)): T {
  return isDerivedGetter(value) ? (value as () => T)() : (value as T);
}
