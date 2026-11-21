import { describe, expect, it, vi } from 'vitest';
import { debounced } from './canvas.js';

describe('debounced', () => {
  // A resize (debounce 0) must reach the renderer in a task of its own, not in
  // the ResizeObserver callback, which runs before the frame is painted.
  it('runs a 0 ms resize in a later task, once for several calls', async () => {
    const fn = vi.fn();
    const run = debounced(fn, 0);
    run();
    run();
    expect(fn).not.toHaveBeenCalled();
    await Promise.resolve();
    expect(fn).not.toHaveBeenCalled();
    await new Promise((r) => setTimeout(r, 10));
    expect(fn).toHaveBeenCalledTimes(1);
    run();
    await new Promise((r) => setTimeout(r, 10));
    expect(fn).toHaveBeenCalledTimes(2);
    run.cancel();
  });

  it('does not run after cancel', async () => {
    const fn = vi.fn();
    const run = debounced(fn, 0);
    run();
    run.cancel();
    await new Promise((r) => setTimeout(r, 10));
    expect(fn).not.toHaveBeenCalled();
  });

  it('waits out a debounce', async () => {
    const fn = vi.fn();
    const run = debounced(fn, 20);
    run();
    await new Promise((r) => setTimeout(r, 5));
    run();
    await new Promise((r) => setTimeout(r, 10));
    expect(fn).not.toHaveBeenCalled();
    await new Promise((r) => setTimeout(r, 30));
    expect(fn).toHaveBeenCalledTimes(1);
  });
});
