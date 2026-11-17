// The engine uses the page runtime from the global object. In a page the
// runtime bundle attaches its whole API to window; the tests do the same with
// the runtime sources (the npm entry exports only the public subset).
import * as signal from '../runtime/src/signal.ts';
import * as effects from '../runtime/src/effects.ts';
import * as dom from '../runtime/src/dom.ts';
import * as list from '../runtime/src/list.ts';
import * as context from '../runtime/src/context.ts';

Object.assign(globalThis, signal, effects, dom, list, context);

// happy-dom has no 2D canvas. Konva only needs a context it can draw into, so
// a stand-in that accepts every call is enough (hit detection reads zeros).
function fakeContext(canvas: HTMLCanvasElement): CanvasRenderingContext2D {
  const state: Record<string | symbol, unknown> = { canvas };
  const noop = (): void => {};
  const special: Record<string, (...args: any[]) => unknown> = {
    getImageData: (_x: number, _y: number, w = 1, h = 1) => ({ data: new Uint8ClampedArray(Math.max(1, w * h * 4)), width: w, height: h }),
    createImageData: (w = 1, h = 1) => ({ data: new Uint8ClampedArray(Math.max(1, w * h * 4)), width: w, height: h }),
    measureText: (text: string) => ({ width: String(text).length * 6, actualBoundingBoxAscent: 8, actualBoundingBoxDescent: 2, fontBoundingBoxAscent: 8, fontBoundingBoxDescent: 2 }),
    createLinearGradient: () => ({ addColorStop: noop }),
    createRadialGradient: () => ({ addColorStop: noop }),
    createConicGradient: () => ({ addColorStop: noop }),
    createPattern: () => ({ setTransform: noop }),
    getLineDash: () => [],
    getTransform: () => ({ a: 1, b: 0, c: 0, d: 1, e: 0, f: 0 }),
    isPointInPath: () => false,
    isPointInStroke: () => false,
  };
  return new Proxy(state, {
    get(target, key) {
      if (key in target) return target[key];
      if (typeof key === 'string' && special[key]) return special[key];
      return noop;
    },
    set(target, key, value) {
      target[key] = value;
      return true;
    },
  }) as unknown as CanvasRenderingContext2D;
}

const contexts = new WeakMap<HTMLCanvasElement, CanvasRenderingContext2D>();
HTMLCanvasElement.prototype.getContext = function (this: HTMLCanvasElement) {
  let ctx = contexts.get(this);
  if (!ctx) contexts.set(this, (ctx = fakeContext(this)));
  return ctx;
} as typeof HTMLCanvasElement.prototype.getContext;
