// Krate-native replacement for `@radix-ui/react-slot` (used by shadcn's
// `asChild` pattern). The compiler aliases the Radix package here so copied
// shadcn source compiles without React or a compiled package dependency.
import { Slot as KrateSlot } from './class-helpers.js';

export const Slot = KrateSlot;
export const Root = KrateSlot;

/** Radix's `Slottable` marker: the wrapped children are rendered in place. */
export function Slottable(props: { children?: unknown }): unknown {
  return props?.children;
}

export default Slot;
