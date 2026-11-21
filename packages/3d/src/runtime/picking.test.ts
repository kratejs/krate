import { describe, expect, it, vi } from 'vitest';
import { createFake } from './__fixtures__/fake.js';
import { Picker } from './picking.js';

function setup(dpr = 2) {
  const fake = createFake();
  const engine = fake.module.Engine.create(document.createElement('canvas'));
  const view = engine.createView();
  const canvas = document.createElement('canvas');
  canvas.width = 800;
  canvas.height = 400;
  canvas.getBoundingClientRect = () => ({ left: 10, top: 20, width: 400, height: 200, right: 410, bottom: 220, x: 10, y: 20 }) as DOMRect;
  const picker = new Picker(view, canvas, () => dpr);
  const send = (type: string, x: number, y: number): void => {
    canvas.dispatchEvent(new MouseEvent(type, { clientX: x, clientY: y }));
  };
  const answer = (renderable: number): void => {
    fake.state.picks.shift()!.cb({ renderable, depth: 0, fragCoords: [] });
  };
  return { fake, picker, send, answer, picks: fake.state.picks, canvas };
}

describe('Picker', () => {
  it('accepts the Entity object the real binding returns, and null for a miss', () => {
    const { fake, picker, send, picks } = setup();
    const hit = vi.fn();
    picker.on(7, 'click', hit);
    send('click', 110, 70);
    picker.update();
    fake.state.picks.shift()!.cb({ renderable: { getId: () => 7 }, depth: 0, fragCoords: [] } as never);
    expect(hit).toHaveBeenCalledTimes(1);
    send('click', 110, 70);
    picker.update();
    picks.shift()!.cb({ renderable: null, depth: 0, fragCoords: [] } as never);
    expect(hit).toHaveBeenCalledTimes(1);
  });

  it('scales by the pixel ratio and flips y to a bottom-left origin', () => {
    const { picker, send, picks } = setup(2);
    picker.on(7, 'click', () => {});
    send('click', 110, 70);
    picker.update();
    // CSS (100, 50) inside the canvas to device (200, 100); y flips against height 400.
    expect(picks).toHaveLength(1);
    expect([picks[0].x, picks[0].y]).toEqual([200, 300]);
  });

  it('keeps at most one pick in flight and issues one per update', () => {
    const { picker, send, answer, picks } = setup();
    picker.on(1, 'pointermove', () => {});
    send('pointermove', 20, 30);
    picker.update();
    send('pointermove', 30, 40);
    send('pointermove', 40, 50);
    picker.update();
    picker.update();
    expect(picks).toHaveLength(1);
    answer(0);
    // Only the latest of the coalesced moves remains.
    picker.update();
    expect(picks).toHaveLength(1);
    expect([picks[0].x, picks[0].y]).toEqual([60, 400 - 60]);
    answer(0);
    picker.update();
    expect(picks).toHaveLength(0);
  });

  it('dispatches click to the picked entity with canvas-relative coordinates', () => {
    const { picker, send, answer } = setup();
    const a = vi.fn();
    const b = vi.fn();
    picker.on(5, 'click', a);
    picker.on(6, 'click', b);
    send('click', 110, 70);
    picker.update();
    answer(6);
    expect(a).not.toHaveBeenCalled();
    expect(b).toHaveBeenCalledOnce();
    expect(b.mock.calls[0][0]).toMatchObject({ entity: 6, x: 100, y: 50 });
  });

  it('ignores clicks on the background and on entities without handlers', () => {
    const { picker, send, answer } = setup();
    const h = vi.fn();
    picker.on(5, 'click', h);
    send('click', 50, 50);
    picker.update();
    answer(0);
    send('click', 50, 50);
    picker.update();
    answer(99);
    expect(h).not.toHaveBeenCalled();
  });

  it('derives over/out from the previously hovered entity', () => {
    const { picker, send, answer } = setup();
    const calls: string[] = [];
    for (const id of [1, 2]) {
      picker.on(id, 'pointerover', () => calls.push(`over${id}`));
      picker.on(id, 'pointerout', () => calls.push(`out${id}`));
      picker.on(id, 'pointermove', () => calls.push(`move${id}`));
    }
    const hover = (id: number): void => {
      send('pointermove', 20, 30);
      picker.update();
      answer(id);
    };
    hover(1);
    hover(1);
    hover(2);
    hover(0);
    hover(2);
    expect(calls).toEqual(['over1', 'move1', 'move1', 'out1', 'over2', 'move2', 'out2', 'over2', 'move2']);
  });

  it('fires out when the pointer leaves the canvas', () => {
    const { picker, send, answer, canvas } = setup();
    const out = vi.fn();
    picker.on(3, 'pointerout', out);
    send('pointermove', 20, 30);
    picker.update();
    answer(3);
    canvas.dispatchEvent(new Event('pointerleave'));
    expect(out).toHaveBeenCalledOnce();
    canvas.dispatchEvent(new Event('pointerleave'));
    expect(out).toHaveBeenCalledOnce();
  });

  it('routes pointerdown and pointerup', () => {
    const { picker, send, answer } = setup();
    const seen: string[] = [];
    picker.on(4, 'pointerdown', () => seen.push('down'));
    picker.on(4, 'pointerup', () => seen.push('up'));
    send('pointerdown', 20, 30);
    picker.update();
    answer(4);
    send('pointerup', 20, 30);
    picker.update();
    answer(4);
    expect(seen).toEqual(['down', 'up']);
  });

  it('drops late pick results after dispose', () => {
    const { picker, send, answer } = setup();
    const h = vi.fn();
    picker.on(4, 'click', h);
    send('click', 20, 30);
    picker.update();
    picker.dispose();
    answer(4);
    expect(h).not.toHaveBeenCalled();
  });
});
