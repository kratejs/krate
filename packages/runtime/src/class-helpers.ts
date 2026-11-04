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
  if (typeof value === 'string') {
    out.push(value);
    return;
  }
  if (typeof value === 'number') {
    // clsx drops every falsy value, so 0 and NaN vanish.
    if (value) out.push(String(value));
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
 * Merge conflicting Tailwind utilities: later classes win over earlier ones in
 * the same utility group and variant chain (`hover:p-2` never conflicts with
 * `p-4`). Classes the helper does not recognise only dedupe on an exact match.
 * The compiler folds literal calls; this covers dynamic joins.
 */
export function twMerge(...inputs: ClassValue[]): string {
  const tokens: string[] = [];
  for (const input of inputs) flattenClass(input, tokens);
  // Split on whitespace so "a b" strings from clsx are handled per class.
  const classes = tokens.flatMap((t) => t.split(/\s+/)).filter(Boolean);
  const byKey = new Map<string, string>();
  for (const token of classes) {
    const { scope, group } = classify(token);
    if (group === null) {
      byKey.delete('\0' + token);
      byKey.set('\0' + token, token);
      continue;
    }
    // Later class wins and takes the later position, like tailwind-merge.
    const key = scope + group;
    byKey.delete(key);
    for (const loser of CONFLICTS[group] ?? []) byKey.delete(scope + loser);
    byKey.set(key, token);
  }
  return Array.from(byKey.values()).join(' ');
}

// Groups a later class of the key group also overrides when seen earlier.
const CONFLICTS: Record<string, string[]> = {
  p: ['px', 'py', 'pt', 'pr', 'pb', 'pl', 'ps', 'pe'],
  px: ['pr', 'pl', 'ps', 'pe'],
  py: ['pt', 'pb'],
  m: ['mx', 'my', 'mt', 'mr', 'mb', 'ml', 'ms', 'me'],
  mx: ['mr', 'ml', 'ms', 'me'],
  my: ['mt', 'mb'],
  inset: ['inset-x', 'inset-y', 'top', 'right', 'bottom', 'left', 'start', 'end'],
  'inset-x': ['right', 'left', 'start', 'end'],
  'inset-y': ['top', 'bottom'],
  gap: ['gap-x', 'gap-y'],
  size: ['w', 'h'],
  overflow: ['overflow-x', 'overflow-y'],
  scale: ['scale-x', 'scale-y'],
  rounded: ['rounded-t', 'rounded-r', 'rounded-b', 'rounded-l', 'rounded-s', 'rounded-e', 'rounded-tl', 'rounded-tr', 'rounded-br', 'rounded-bl', 'rounded-ss', 'rounded-se', 'rounded-ee', 'rounded-es'],
  'rounded-t': ['rounded-tl', 'rounded-tr'],
  'rounded-r': ['rounded-tr', 'rounded-br'],
  'rounded-b': ['rounded-br', 'rounded-bl'],
  'rounded-l': ['rounded-tl', 'rounded-bl'],
  'rounded-s': ['rounded-ss', 'rounded-es'],
  'rounded-e': ['rounded-se', 'rounded-ee'],
  'border-w': ['border-w-x', 'border-w-y', 'border-w-t', 'border-w-r', 'border-w-b', 'border-w-l', 'border-w-s', 'border-w-e'],
  'border-w-x': ['border-w-r', 'border-w-l', 'border-w-s', 'border-w-e'],
  'border-w-y': ['border-w-t', 'border-w-b'],
  'border-color': ['border-color-x', 'border-color-y', 'border-color-t', 'border-color-r', 'border-color-b', 'border-color-l', 'border-color-s', 'border-color-e'],
  'border-color-x': ['border-color-r', 'border-color-l', 'border-color-s', 'border-color-e'],
  'border-color-y': ['border-color-t', 'border-color-b'],
  'font-size': ['leading'],
};

/** Split on `sep` outside [] and () so arbitrary values stay intact. */
function splitTop(s: string, sep: string): string[] {
  const out: string[] = [];
  let depth = 0;
  let start = 0;
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (c === '[' || c === '(') depth++;
    else if (c === ']' || c === ')') depth = Math.max(0, depth - 1);
    else if (c === sep && depth === 0) {
      out.push(s.slice(start, i));
      start = i + 1;
    }
  }
  out.push(s.slice(start));
  return out;
}

const LEN_HINT = /^(length|size|percentage|number|position|image|url|family|weight|line-width|bg-size)$/;

/** Kind of an arbitrary or variable value: a type hint, or a guess. */
type ValueKind = 'length' | 'color' | 'number' | 'other' | 'unknown';

function arbitraryKind(value: string): ValueKind | null {
  let inner: string;
  if (value.startsWith('[') && value.endsWith(']')) inner = value.slice(1, -1);
  else if (value.startsWith('(') && value.endsWith(')')) inner = value.slice(1, -1);
  else return null;
  const m = /^([a-z-]+):(.*)$/.exec(inner);
  if (m && !m[2].startsWith(':')) {
    const hint = m[1];
    if (hint === 'color') return 'color';
    if (hint === 'number' || hint === 'weight') return 'number';
    if (LEN_HINT.test(hint)) return hint === 'family' || hint === 'image' || hint === 'url' ? 'other' : 'length';
  }
  if (value.startsWith('(')) return 'unknown';
  const v = inner.replace(/_/g, ' ');
  if (/^(#|rgb|hsl|hwb|lab|lch|oklab|oklch|color\()/i.test(v)) return 'color';
  if (/^(calc|min|max|clamp)\(/.test(v) || /^[+-]?(\d+\.?\d*|\.\d+)(%|[a-z]+)$/i.test(v)) return 'length';
  if (/^[+-]?(\d+\.?\d*|\.\d+)$/.test(v)) return 'number';
  return 'unknown';
}

const isNumeric = (v: string): boolean => /^[+-]?(\d+\.?\d*|\.\d+)$/.test(v) || /^\d+\/\d+$/.test(v);

// Groups whose every value collapses into one group. Longest head wins.
const SIMPLE = new Set([
  'p', 'px', 'py', 'pt', 'pr', 'pb', 'pl', 'ps', 'pe',
  'm', 'mx', 'my', 'mt', 'mr', 'mb', 'ml', 'ms', 'me',
  'gap', 'gap-x', 'gap-y', 'space-x', 'space-y',
  'inset', 'inset-x', 'inset-y', 'top', 'right', 'bottom', 'left', 'start', 'end',
  'z', 'order', 'opacity', 'duration', 'delay', 'ease', 'animate',
  'basis', 'grow', 'shrink', 'w', 'h', 'size', 'min-w', 'max-w', 'min-h', 'max-h',
  'grid-cols', 'grid-rows', 'col-span', 'col-start', 'col-end', 'row-span', 'row-start', 'row-end',
  'leading', 'tracking', 'line-clamp', 'columns', 'aspect',
  'blur', 'brightness', 'contrast', 'grayscale', 'hue-rotate', 'invert', 'saturate', 'sepia',
  'backdrop-blur', 'backdrop-brightness', 'backdrop-contrast', 'backdrop-grayscale',
  'backdrop-hue-rotate', 'backdrop-invert', 'backdrop-opacity', 'backdrop-saturate', 'backdrop-sepia',
  'scale', 'scale-x', 'scale-y', 'rotate', 'translate-x', 'translate-y', 'skew-x', 'skew-y',
  'origin', 'cursor', 'select', 'underline-offset', 'indent', 'perspective', 'will-change',
  'overflow', 'overflow-x', 'overflow-y', 'overscroll', 'pointer-events', 'resize', 'whitespace',
  'rounded', 'rounded-t', 'rounded-r', 'rounded-b', 'rounded-l', 'rounded-s', 'rounded-e',
  'rounded-tl', 'rounded-tr', 'rounded-br', 'rounded-bl', 'rounded-ss', 'rounded-se', 'rounded-ee', 'rounded-es',
  'transition',
]);

const FONT_WEIGHTS = new Set(['thin', 'extralight', 'light', 'normal', 'medium', 'semibold', 'bold', 'extrabold', 'black']);
const TEXT_SIZES = /^(xs|sm|base|lg|xl|\d+xl)$/;
const BORDER_STYLES = new Set(['solid', 'dashed', 'dotted', 'double', 'hidden', 'none']);
const BG_POSITIONS = new Set(['bottom', 'center', 'left', 'left-bottom', 'left-top', 'right', 'right-bottom', 'right-top', 'top']);

// Standalone (value-less) utilities mapped to a group.
const STANDALONE: Record<string, string> = {};
for (const d of ['block', 'inline-block', 'inline', 'flex', 'inline-flex', 'table', 'inline-table', 'table-caption', 'table-cell', 'table-column', 'table-column-group', 'table-footer-group', 'table-header-group', 'table-row-group', 'table-row', 'flow-root', 'grid', 'inline-grid', 'contents', 'list-item', 'hidden']) STANDALONE[d] = 'display';
for (const d of ['static', 'fixed', 'absolute', 'relative', 'sticky']) STANDALONE[d] = 'position';
for (const d of ['visible', 'invisible', 'collapse']) STANDALONE[d] = 'visibility';
for (const d of ['italic', 'not-italic']) STANDALONE[d] = 'font-style';
for (const d of ['uppercase', 'lowercase', 'capitalize', 'normal-case']) STANDALONE[d] = 'text-transform';
for (const d of ['underline', 'overline', 'line-through', 'no-underline']) STANDALONE[d] = 'text-decoration';
for (const d of ['flex-row', 'flex-row-reverse', 'flex-col', 'flex-col-reverse']) STANDALONE[d] = 'flex-direction';
for (const d of ['flex-wrap', 'flex-nowrap', 'flex-wrap-reverse']) STANDALONE[d] = 'flex-wrap';
STANDALONE['truncate'] = 'truncate';

function classify(token: string): { scope: string; group: string | null } {
  const parts = splitTop(token, ':');
  let base = parts.pop() as string;
  const variants = parts.sort();
  let important = '';
  if (base.startsWith('!')) {
    important = '!';
    base = base.slice(1);
  }
  if (base.endsWith('!')) {
    important = '!';
    base = base.slice(0, -1);
  }
  const scope = variants.join(':') + ':' + important;
  if (base.startsWith('-')) base = base.slice(1);
  // Arbitrary property: [mask-type:alpha]
  if (base.startsWith('[')) {
    const m = /^\[([a-z-]+):/.exec(base);
    return { scope, group: m ? 'arb:' + m[1] : null };
  }
  // Opacity modifier or fraction: drop a trailing /value.
  const slash = splitTop(base, '/');
  if (slash.length > 1) base = slash.slice(0, -1).join('/');
  const group = utilityGroup(base);
  return { scope, group };
}

/** Longest dash-separated head of `base` found in `set`, with the rest. */
function splitHead(base: string, set: Set<string>): [string, string] | null {
  const segs = splitTop(base, '-');
  for (let n = Math.min(segs.length, 3); n >= 1; n--) {
    const head = segs.slice(0, n).join('-');
    if (set.has(head)) return [head, segs.slice(n).join('-')];
  }
  return null;
}

function utilityGroup(base: string): string | null {
  if (STANDALONE[base]) return STANDALONE[base];
  const segs = splitTop(base, '-');
  const head = segs[0];
  const rest = segs.slice(1).join('-');

  // Colour/size/width/style families that share a head.
  switch (head) {
    case 'text': {
      if (rest === '') return null;
      if (TEXT_SIZES.test(rest)) return 'font-size';
      if (['left', 'center', 'right', 'justify', 'start', 'end'].includes(rest)) return 'text-align';
      if (['wrap', 'nowrap', 'balance', 'pretty'].includes(rest)) return 'text-wrap';
      if (['ellipsis', 'clip'].includes(rest)) return 'text-overflow';
      const k = arbitraryKind(rest);
      if (k === 'length') return 'font-size';
      return 'text-color';
    }
    case 'font': {
      const k = arbitraryKind(rest);
      if (FONT_WEIGHTS.has(rest) || k === 'number') return 'font-weight';
      if (rest.startsWith('stretch')) return 'font-stretch';
      return 'font-family';
    }
    case 'bg': {
      if (rest === '') return null;
      if (['auto', 'cover', 'contain'].includes(rest)) return 'bg-size';
      if (BG_POSITIONS.has(rest)) return 'bg-position';
      if (/^(repeat|no-repeat)/.test(rest)) return 'bg-repeat';
      if (['fixed', 'local', 'scroll'].includes(rest)) return 'bg-attachment';
      if (rest.startsWith('clip-')) return 'bg-clip';
      if (rest.startsWith('origin-')) return 'bg-origin';
      if (rest.startsWith('blend-')) return 'bg-blend';
      if (rest === 'none' || /^(gradient-to|linear|radial|conic)/.test(rest)) return 'bg-image';
      const k = arbitraryKind(rest);
      if (k === 'length') return 'bg-size';
      if (rest.startsWith('[') && /^\[(position|percentage):/.test(rest)) return 'bg-position';
      if (rest.startsWith('[') && (/^\[(image|url):/.test(rest) || rest.startsWith('[url('))) return 'bg-image';
      return 'bg-color';
    }
    case 'border': {
      return widthColorStyle(rest, 'border');
    }
    case 'divide': {
      if (rest === 'x' || rest === 'y' || rest.startsWith('x-') || rest.startsWith('y-')) return 'divide-' + rest[0];
      if (rest === 'reverse' || rest.endsWith('-reverse')) return 'divide-reverse';
      if (BORDER_STYLES.has(rest)) return 'divide-style';
      return 'divide-color';
    }
    case 'outline': {
      if (rest.startsWith('offset')) return 'outline-offset';
      return widthColorStyle(rest, 'outline');
    }
    case 'ring': {
      if (rest.startsWith('offset-')) {
        const r = rest.slice('offset-'.length);
        const k = arbitraryKind(r);
        return isNumeric(r) || k === 'length' || k === 'number' ? 'ring-offset-w' : 'ring-offset-color';
      }
      if (rest === 'inset') return 'ring-inset';
      if (rest === '') return 'ring-w';
      const k = arbitraryKind(rest);
      return isNumeric(rest) || k === 'length' || k === 'number' ? 'ring-w' : 'ring-color';
    }
    case 'shadow': {
      if (rest === '') return 'shadow';
      const k = arbitraryKind(rest);
      if (k === 'color') return 'shadow-color';
      if (k === 'length' || k === 'number' || k === 'other' || (k === 'unknown' && rest.startsWith('['))) return 'shadow';
      if (/^(2xs|xs|sm|md|lg|xl|2xl|none|inner)$/.test(rest)) return 'shadow';
      return 'shadow-color';
    }
    case 'stroke': {
      const k = arbitraryKind(rest);
      return isNumeric(rest) || k === 'length' || k === 'number' ? 'stroke-w' : 'stroke-color';
    }
    case 'fill':
      return 'fill';
    case 'decoration': {
      if (['solid', 'double', 'dotted', 'dashed', 'wavy'].includes(rest)) return 'decoration-style';
      const k = arbitraryKind(rest);
      if (isNumeric(rest) || k === 'length' || k === 'number' || rest === 'from-font' || rest === 'auto') return 'decoration-thickness';
      return 'decoration-color';
    }
    case 'object':
      return ['contain', 'cover', 'fill', 'none', 'scale-down'].includes(rest) ? 'object-fit' : 'object-position';
    case 'list':
      if (rest === 'inside' || rest === 'outside') return 'list-position';
      if (rest.startsWith('image')) return 'list-image';
      return 'list-style-type';
    case 'flex':
      if (rest === '') return null;
      return 'flex';
    case 'grid':
      if (rest.startsWith('flow')) return 'grid-flow';
      break;
    case 'shrink':
    case 'grow':
      return head;
    case 'transition':
      return 'transition';
    case 'inset':
      break;
  }

  const hit = splitHead(base, SIMPLE);
  if (hit) {
    return hit[0];
  }
  return null;
}

/** Group for border/outline: width, style or colour, with optional side. */
function widthColorStyle(rest: string, name: string): string | null {
  let side = '';
  let value = rest;
  const m = /^(x|y|t|r|b|l|s|e)(?:-(.*))?$/.exec(rest);
  if (name === 'border' && m) {
    side = '-' + m[1];
    value = m[2] ?? '';
  }
  if (name === 'border' && (rest === 'collapse' || rest === 'separate')) return 'border-collapse';
  if (name === 'border' && rest.startsWith('spacing')) return 'border-spacing';
  if (value === '') return name === 'border' ? 'border-w' + side : name + '-w';
  if (BORDER_STYLES.has(value)) return name === 'border' ? 'border-style' : name === 'outline' && value !== 'hidden' ? 'outline-style' : name + '-style';
  const k = arbitraryKind(value);
  if (isNumeric(value) || k === 'length' || k === 'number') return name === 'border' ? 'border-w' + side : name + '-w';
  return name === 'border' ? 'border-color' + side : name + '-color';
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
