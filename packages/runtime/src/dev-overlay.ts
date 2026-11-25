/**
 * Dev-only overlay: the dock, its panel, the error overlay, the command menu
 * and the component inspector.
 *
 * Bundled separately as `krate-dev.js` and injected only by `krate dev`; it is
 * never part of the production runtime. The modules live in ./dev/.
 */

import { KrateDev } from './dev/app.js';

export { KrateDev } from './dev/app.js';
export { isNativeOrigin, routeMatches } from './dev/util.js';
export { decodeVLQ, decodeMappings, parseStackFrame, symbolicateStack } from './dev/sourcemap.js';
export { parseFrames, hostStackOf, hostEventLine, formatBytes, jsHeapBytes, memoryRows, type MemoryReport } from './dev/native.js';

/** Initialise the dev overlay. Safe to call more than once. */
export function initDevOverlay(): KrateDev | undefined {
  if (typeof window === 'undefined' || typeof document === 'undefined') return undefined;
  if ((window as any).__krate_dev_overlay) return undefined;
  (window as any).__krate_dev_overlay = true;
  try {
    const app = new KrateDev();
    app.init();
    // The instance, for debugging from the console and for browser tests.
    (window as any).__krate_dev_overlay = app;
    return app;
  } catch (err) {
    // Never let the dev tooling break the app.
    console.error('[krate] dev overlay failed to initialise', err);
    return undefined;
  }
}

if (typeof window !== 'undefined' && typeof document !== 'undefined') {
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => initDevOverlay(), { once: true });
  } else {
    initDevOverlay();
  }
}
