// Escaping for values substituted into scaffold templates. A project name is
// arbitrary user input (a directory name can hold quotes, braces and angle
// brackets), so it must be made safe for the kind of file it lands in.

const JSX_FILES = new Set(['.tsx', '.jsx']);
const JS_STRING_FILES = new Set(['.ts', '.js', '.mjs', '.cjs']);

/** Escape text for use as JSX children (`<span>TEXT</span>`). */
export function escapeJsxText(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/\{/g, '&#123;')
    .replace(/\}/g, '&#125;');
}

/** Escape text for use inside a double-quoted JS/TS or JSON string literal. */
export function escapeStringLiteral(value: string): string {
  return JSON.stringify(value).slice(1, -1);
}

/** Escape the display name for a template file with the given extension. */
export function escapeDisplayName(ext: string, value: string): string {
  const e = ext.toLowerCase();
  if (JSX_FILES.has(e)) return escapeJsxText(value);
  if (JS_STRING_FILES.has(e) || e === '.json') return escapeStringLiteral(value);
  return value;
}
