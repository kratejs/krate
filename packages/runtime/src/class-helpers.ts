// Class-name and slot helpers used by shadcn/ui-style components. These are
// small, pure, dependency-free implementations of the common `clsx`, `cn`,
// `cva`, and `Slot`/`asChild` APIs so copied components work without pulling in
// those npm packages. The compiler folds literal calls at build time; these
// runtime versions cover dynamic cases.

export type ClassValue =
  | string
  | number
  | boolean
  | null
  | undefined
  | ClassValue[]
  | Record<string, unknown>;

/** Flatten a clsx-style value into an array of class tokens. */
function flattenClass(value: ClassValue, out: string[]): void {
  if (value == null || value === false || value === true || value === '') return;
  if (typeof value === 'string' || typeof value === 'number') {
    out.push(String(value));
    return;
  }
  if (Array.isArray(value)) {
    for (const item of value) flattenClass(item as ClassValue, out);
    return;
  }
  if (typeof value === 'object') {
    for (const key of Object.keys(value)) {
      if ((value as Record<string, unknown>)[key]) out.push(key);
    }
  }
}

/** `clsx` - conditional class names from strings, arrays, and objects. */
export function clsx(...inputs: ClassValue[]): string {
  const out: string[] = [];
  for (const input of inputs) flattenClass(input, out);
  return out.join(' ');
}

/**
 * Merge conflicting Tailwind utilities. This is a deliberately small subset:
 * later classes win over earlier ones for the same property prefix. Full
 * `tailwind-merge` semantics (variant/modifier aware) are out of scope; the
 * compiler folds literal calls and this covers dynamic joins.
 */
export function twMerge(...inputs: ClassValue[]): string {
  const tokens: string[] = [];
  for (const input of inputs) flattenClass(input, tokens);
  const byProperty = new Map<string, string>();
  const order: string[] = [];
  for (const token of tokens) {
    const key = tailwindPropertyKey(token);
    if (!byProperty.has(key)) order.push(key);
    byProperty.set(key, token);
  }
  return order.map((k) => byProperty.get(k)!).join(' ');
}

/**
 * Best-effort property key for a Tailwind class: the leading property/utility
 * segment before a size/colour value, so `px-2` and `px-4` collide but
 * `px-2` and `py-2` do not.
 */
function tailwindPropertyKey(token: string): string {
  let t = token;
  let prefix = '';
  // Strip a variant chain (hover:, md:, focus: - anything before the last `:`).
  const colon = t.lastIndexOf(':');
  if (colon >= 0) {
    prefix = t.slice(0, colon + 1);
    t = t.slice(colon + 1);
  }
  const dash = t.indexOf('-');
  const base = dash >= 0 ? t.slice(0, dash) : t;
  const scale = dash >= 0 ? t.slice(dash + 1, dash + 2) : '';
  return prefix + base + (scale && /[0-9]/.test(scale) ? '-' + scale : '');
}

/** `cn` - `twMerge` over `clsx`, the shadcn/ui convention. */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(...inputs));
}

export interface CVAConfig {
  variants?: Record<string, Record<string, string>>;
  defaultVariants?: Record<string, string | null>;
  compoundVariants?: Array<Record<string, unknown>>;
}

/**
 * `class-variance-authority` subset. Returns a function that, given a variant
 * selection, produces a class string.
 */
export function cva(base: string, config?: CVAConfig): (props?: Record<string, unknown>) => string {
  const variants = config?.variants ?? {};
  const defaults = config?.defaultVariants ?? {};
  return (props?: Record<string, unknown>): string => {
    const selection: Record<string, string> = {};
    for (const name of Object.keys(variants)) {
      const chosen = props?.[name] ?? defaults[name];
      if (chosen != null) selection[name] = String(chosen);
    }
    const parts = [base];
    for (const name of Object.keys(variants)) {
      const variant = selection[name];
      if (variant && variants[name][variant]) parts.push(variants[name][variant]);
    }
    for (const compound of config?.compoundVariants ?? []) {
      if (matchesCompound(compound, selection, defaults)) {
        const cls = compound['class'];
        if (typeof cls === 'string') parts.push(cls);
      }
    }
    const className = props?.['class'] ?? props?.['className'];
    if (typeof className === 'string') parts.push(className);
    return twMerge(clsx(parts));
  };
}

function matchesCompound(
  compound: Record<string, unknown>,
  selection: Record<string, string>,
  defaults: Record<string, string | null>
): boolean {
  for (const key of Object.keys(compound)) {
    if (key === 'class') continue;
    const want = compound[key];
    const got = selection[key] ?? defaults[key] ?? undefined;
    if (Array.isArray(want)) {
      if (!want.includes(got)) return false;
    } else if (want !== got) {
      return false;
    }
  }
  return true;
}

/**
 * `Slot` - renders its single child element instead of wrapping it, merging the
 * Slot's own props onto that child. This is the `asChild` mechanic from
 * shadcn/radix: `<Slot className="x"><a /></Slot>` returns the `<a>` with `x`
 * merged onto it.
 *
 * In Krate's DOM-node JSX model the child is already a built node, so this
 * applies the remaining props (minus `children`) to it and returns it.
 */
export function Slot(props: Record<string, unknown> & { children?: unknown }): Node | null {
  const child = firstNode(props?.children);
  if (!child) return null;
  applySlotProps(child, props);
  return child;
}

function firstNode(children: unknown): Node | null {
  if (children == null) return null;
  if (Array.isArray(children)) {
    for (const c of children) {
      const n = firstNode(c);
      if (n) return n;
    }
    return null;
  }
  if (children instanceof Node) return children;
  return null;
}

function applySlotProps(node: Node, props: Record<string, unknown>): void {
  if (!(node instanceof Element)) return;
  for (const key of Object.keys(props)) {
    if (key === 'children' || key === 'ref') continue;
    const value = props[key];
    if (value == null || value === false) continue;
    if (key === 'class' || key === 'className') {
      const existing = node.getAttribute('class');
      const merged = existing ? existing + ' ' + String(value) : String(value);
      node.setAttribute('class', merged);
      continue;
    }
    if (key === 'style' && typeof value === 'object') {
      for (const [prop, val] of Object.entries(value as Record<string, unknown>)) {
        (node as HTMLElement).style.setProperty(kebab(prop), String(val));
      }
      continue;
    }
    if (key.startsWith('on') && typeof value === 'function') {
      (node as unknown as Record<string, unknown>)[key] = value;
      continue;
    }
    if (value === true) {
      node.setAttribute(key, '');
      continue;
    }
    node.setAttribute(key, String(value));
  }
}

function kebab(name: string): string {
  return name.replace(/[A-Z]/g, (m) => '-' + m.toLowerCase());
}

/**
 * `cloneElement` - apply extra props (and optional replacement children) to an
 * existing node and return it. Used by `Slot`/`asChild` and by libraries that
 * clone a single child.
 */
export function cloneElement(
  element: Node,
  props?: Record<string, unknown> | null,
  ...children: unknown[]
): Node {
  if (!(element instanceof Node)) return element;
  if (props) applySlotProps(element, props);
  if (children.length > 0 && element instanceof Element) {
    element.textContent = '';
    for (const child of children) {
      if (child instanceof Node) element.appendChild(child);
      else if (child != null) element.appendChild(document.createTextNode(String(child)));
    }
  }
  return element;
}
