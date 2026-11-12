import { describe, expect, expectTypeOf, it } from 'vitest';
import { defineConfig } from '../src/index.js';
import type { KrateConfig, MotionConfig, MotionTransitionConfig } from '../src/index.js';

describe('motion config types', () => {
  it('accepts true and a full object', () => {
    const simple = defineConfig({ motion: true });
    expect(simple.motion).toBe(true);

    const full = defineConfig({
      motion: {
        reducedMotion: 'user',
        declarative: true,
        transition: { type: 'spring', stiffness: 200, damping: 20, ease: 'snappy' },
        viewport: { amount: 0.3, margin: '0px 0px -10%', once: true },
        stagger: { delay: 0.1, from: 'center' },
        easings: { snappy: [0.2, 0.9, 0.3, 1] },
        presets: {
          pop: { initial: { scale: 0.8, opacity: 0 }, animate: { scale: [0.8, 1.05, 1], opacity: 1 } },
        },
      },
    });
    expect((full.motion as MotionConfig).presets?.pop?.initial?.scale).toBe(0.8);
  });

  it('is part of KrateConfig', () => {
    expectTypeOf<KrateConfig['motion']>().toEqualTypeOf<boolean | MotionConfig | undefined>();
    expectTypeOf<MotionConfig['reducedMotion']>().toEqualTypeOf<'user' | 'always' | 'never' | undefined>();
    expectTypeOf<MotionTransitionConfig['type']>().toEqualTypeOf<'spring' | 'tween' | 'inertia' | undefined>();
  });

  it('rejects unknown keys and bad values at compile time', () => {
    // @ts-expect-error unknown key
    defineConfig({ motion: { reducedMotoin: 'user' } });
    // @ts-expect-error invalid reduced motion value
    defineConfig({ motion: { reducedMotion: 'sometimes' } });
    // @ts-expect-error easing points must be a 4-tuple
    defineConfig({ motion: { easings: { bad: [0, 1] } } });
    expect(true).toBe(true);
  });
});
