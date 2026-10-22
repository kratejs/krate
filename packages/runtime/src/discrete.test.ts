import { describe, it, expect, beforeEach } from 'vitest';
import { krateOn } from './events';
import { createSignal, createEffect, disposeAll, krateHookState, krateFlushSync } from './signal';
import { createEffectDeps } from './effects';

const tick = () => new Promise((r) => setTimeout(r, 0));

function mount(html: string): HTMLElement {
  document.body.innerHTML = '<div id="root">' + html + '</div>';
  return document.getElementById('root')!;
}

describe('discrete events flush synchronously', () => {
  beforeEach(() => disposeAll());

  it('commits render, layout and passive effects before the native event returns', async () => {
    mount('<button id="b">go</button><p id="p"></p>');
    const p = document.getElementById('p')!;
    const [n, setN] = createSignal(0, krateHookState);
    const log: string[] = [];
    createEffect(() => {
      p.textContent = String(n());
    });
    createEffectDeps(() => void log.push('layout ' + n()), () => [n()], 'layout');
    createEffectDeps(() => void log.push('passive ' + n()), () => [n()]);
    await tick();
    await tick();
    log.length = 0;
    krateOn(document.getElementById('b')!, 'onClick', () => {
      setN(1);
      setN(2);
      queueMicrotask(() => log.push('microtask'));
    });
    document.getElementById('b')!.click();
    // One flush after all handlers ran: the batched value, effects included.
    expect(p.textContent).toBe('2');
    expect(log).toEqual(['layout 2', 'passive 2']);
    await tick();
    expect(log).toEqual(['layout 2', 'passive 2', 'microtask']);
  });

  it('flushes once per native event after capture and bubble handlers', () => {
    mount('<div id="outer"><button id="b">go</button></div>');
    const [n, setN] = createSignal(0, krateHookState);
    const renders: number[] = [];
    createEffect(() => void renders.push(n()));
    krateOn(document.getElementById('outer')!, 'onClickCapture', () => setN((v) => v + 1));
    krateOn(document.getElementById('b')!, 'onClick', () => setN((v) => v + 1));
    krateOn(document.getElementById('outer')!, 'onClick', () => setN((v) => v + 1));
    document.getElementById('b')!.click();
    expect(renders).toEqual([0, 3]);
  });

  it('folds an event fired during the flush (blur of a removed node) into the same commit', () => {
    mount('<button id="b">go</button><input id="i">');
    const input = document.getElementById('i')!;
    const [a, setA] = createSignal(0, krateHookState);
    const [blurs, setBlurs] = createSignal(0, krateHookState);
    const commits: string[] = [];
    krateOn(input, 'onBlur', () => setBlurs((v) => v + 1));
    createEffect(() => {
      // A binding that, like removing a focused node, fires a native blur.
      if (a() === 1) input.dispatchEvent(new FocusEvent('focusout', { bubbles: true }));
    });
    createEffectDeps(() => void commits.push(a() + ':' + blurs()), () => [a(), blurs()], 'layout');
    krateOn(document.getElementById('b')!, 'onClick', () => setA(1));
    krateFlushSync();
    commits.length = 0;
    document.getElementById('b')!.click();
    expect(commits).toEqual(['1:1']);
  });

  it('keeps continuous events batched', async () => {
    mount('<div id="area"></div>');
    const area = document.getElementById('area')!;
    const [n, setN] = createSignal(0, krateHookState);
    const seen: number[] = [];
    createEffect(() => void seen.push(n()));
    krateOn(area, 'onMouseMove', () => setN(5));
    area.dispatchEvent(new MouseEvent('mousemove', { bubbles: true }));
    expect(seen).toEqual([0]);
    await tick();
    await tick();
    expect(seen).toEqual([0, 5]);
  });
});
