// Inline style of the <canvas> element, shared by the runtime and compiled Canvas.

import type { CanvasProps } from '../types.js';

/** Inline style for the <canvas>: fills its parent unless overridden. */
export function canvasStyle(style: CanvasProps['style']): string {
  const base = 'display:block;width:100%;height:100%;';
  if (!style) return base;
  if (typeof style === 'string') return base + style;
  return base + Object.entries(style).map(([k, v]) => `${k.replace(/[A-Z]/g, (c) => `-${c.toLowerCase()}`)}:${v}`).join(';');
}
