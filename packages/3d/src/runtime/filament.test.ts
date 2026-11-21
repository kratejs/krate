import { afterEach, describe, expect, it, vi } from 'vitest';
import { loadFilament, type FilamentModule } from './filament.js';

type G = { Filament?: unknown };

function stub(): FilamentModule {
  return { Engine: {}, init: (_a: string[], cb?: (() => void) | null) => cb?.() } as unknown as FilamentModule;
}

afterEach(() => {
  delete (globalThis as G).Filament;
  vi.restoreAllMocks();
});

describe('loadFilament', () => {
  it('uses an injected module and never touches the page', async () => {
    const append = vi.spyOn(document.head, 'appendChild');
    const mod = stub();
    expect(await loadFilament(mod, '/assets/3d/filament-1/filament.js')).toBe(mod);
    expect(await loadFilament(() => mod)).toBe(mod);
    expect(append).not.toHaveBeenCalled();
  });

  it('tells the author to install filament when there is no source and no URL', async () => {
    await expect(loadFilament()).rejects.toThrow(/npm install filament/);
  });

  it('loads filament.js as a classic script and uses the global it defines', async () => {
    const mod = stub();
    let script: HTMLScriptElement | undefined;
    vi.spyOn(document.head, 'appendChild').mockImplementation((node) => {
      script = node as HTMLScriptElement;
      queueMicrotask(() => {
        (globalThis as G).Filament = mod;
        script!.onload?.(new Event('load'));
      });
      return node;
    });
    expect(await loadFilament(undefined, '/assets/3d/filament-9/filament.js')).toBe(mod);
    expect(script?.getAttribute('src')).toBe('/assets/3d/filament-9/filament.js');
    expect(script?.type).not.toBe('module');
  });

  it('reports a script that fails to load', async () => {
    vi.spyOn(document.head, 'appendChild').mockImplementation((node) => {
      queueMicrotask(() => (node as HTMLScriptElement).onerror?.(new Event('error')));
      return node;
    });
    await expect(loadFilament(undefined, '/missing/filament.js')).rejects.toThrow(/could not load Filament from \/missing\/filament.js/);
  });
});
