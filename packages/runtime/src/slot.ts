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

type SlotProps = Record<string, unknown> & { children?: unknown };

/**
 * Radix's `createSlot(ownerName)` (react-slot >= 1.2): a Slot that merges its
 * props and `ref` onto its single child. `ownerName` only labels the component.
 */
export function createSlot(ownerName: string): (props: SlotProps) => Node | null {
  const OwnerSlot = (props: SlotProps): Node | null => {
    const node = KrateSlot(props);
    const ref = props?.ref;
    if (node && typeof ref === 'function') ref(node);
    else if (node && ref && typeof ref === 'object') (ref as { current: unknown }).current = node;
    return node;
  };
  OwnerSlot.displayName = ownerName + '.Slot';
  return OwnerSlot;
}

/** Radix's `createSlottable(ownerName)`: a `Slottable` marker for one owner. */
export function createSlottable(ownerName: string): (props: { children?: unknown }) => unknown {
  const OwnerSlottable = (props: { children?: unknown }): unknown => props?.children;
  OwnerSlottable.displayName = ownerName + '.Slottable';
  return OwnerSlottable;
}

export default Slot;
