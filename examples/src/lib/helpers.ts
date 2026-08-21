export function greet(name: string): string {
  return `Hello, ${name}!`;
}

export function sum(...nums: number[]): number {
  return nums.reduce((acc, n) => acc + n, 0);
}

export function joinList(items: string[], sep: string = ", "): string {
  return items.join(sep);
}

export function clamp(value: number, min: number, max: number): number {
  if (value < min) return min;
  if (value > max) return max;
  return value;
}

export function pick(obj: Record<string, string>, keys: string[]): string[] {
  const out: string[] = [];
  for (const k of keys) {
    if (k in obj) {
      out.push(obj[k]);
    }
  }
  return out;
}

export const DEFAULT_LIMIT = 10;
export const FRAMEWORK_NAME = "krate";