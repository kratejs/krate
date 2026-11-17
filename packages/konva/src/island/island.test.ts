import type Konva from 'konva';
import { createContext, createElement as h, render, type VNode } from 'preact';
import { forwardRef } from 'preact/compat';
import { useContext, useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { act } from 'preact/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';
import * as engine from '../engine/index.js';
import { Circle, Group, Layer as KLayer, Line, Rect, Stage, Text, Transformer, useContextBridge, useStrictMode, version } from './index.js';

// The island build renders a stage with Preact through the engine's host:
// Preact creates host nodes (the konva namespace) and the island applies each
// element's props to its Konva node once Preact has diffed it.

const E = h as unknown as (type: unknown, props?: Record<string, unknown> | null, ...children: unknown[]) => VNode;
const containers: HTMLElement[] = [];

afterEach(() => {
  for (const c of containers.splice(0)) act(() => render(null, c));
  useStrictMode(false);
});

function mount(vnode: VNode): HTMLElement {
  const el = document.createElement('div');
  document.body.appendChild(el);
  containers.push(el);
  act(() => render(vnode, el));
  return el;
}

const names = (c: Konva.Container): string[] => c.children.map((n) => n.name());

describe('@krate/konva in a Preact island', () => {
  it('renders a container div, a Konva.Stage for the ref and the children', () => {
    const ref = { current: null as Konva.Stage | null };
    const el = mount(
      E(Stage, { ref, width: 200, height: 100, className: 'canvas', style: { cursor: 'grab' } }, E(KLayer, null, E(Rect, { name: 'r', x: 5, width: 10, height: 10, fill: 'red', dash: [4, 4], listening: false, hitStrokeWidth: 6, shadowBlur: 2 }))),
    );
    const div = el.firstChild as HTMLDivElement;
    expect(div.className).toBe('canvas');
    expect(div.style.cursor).toBe('grab');
    const stage = ref.current!;
    expect(stage.getClassName()).toBe('Stage');
    expect(stage.container()).toBe(div);
    expect(stage.width()).toBe(200);
    const rect = stage.findOne('.r') as Konva.Rect;
    expect(rect.x()).toBe(5);
    expect(rect.fill()).toBe('red');
    expect(rect.dash()).toEqual([4, 4]);
    expect(rect.listening()).toBe(false);
    expect(rect.hitStrokeWidth()).toBe(6);
    expect(rect.shadowBlur()).toBe(2);
    expect(version).toBe('19.3.2');
  });

  it('updates props, sets removed props to undefined and rebinds changed handlers only', async () => {
    let set!: (n: number) => void;
    const first = vi.fn();
    const second = vi.fn();
    const stable = vi.fn();
    function Scene() {
      const [n, setN] = useState(1);
      set = setN;
      return E(
        Stage,
        { width: 100, height: 100, scaleX: n },
        E(KLayer, null, E(Rect, { name: 'r', x: n, ...(n === 1 ? { fill: 'red', onClick: first } : { onClick: second }), onMouseEnter: stable })),
      );
    }
    const el = mount(E(Scene));
    const stage = (Konva_stage(el));
    const rect = stage.findOne('.r') as Konva.Rect;
    const on = vi.spyOn(rect, 'on');
    act(() => set(2));
    expect(stage.findOne('.r')).toBe(rect);
    expect(rect.x()).toBe(2);
    expect(rect.fill()).toBeUndefined();
    expect(stage.scaleX()).toBe(2);
    expect(on.mock.calls.map((c) => c[0])).toEqual(['click.react-konva-event']);
    rect.fire('click', { evt: {} });
    rect.fire('mouseenter', { evt: {} });
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
    expect(stable).toHaveBeenCalledTimes(1);
  });

  it('keeps node order with keyed lists and conditionals', () => {
    let set!: (v: { items: number[]; mid: boolean }) => void;
    function Scene() {
      const [s, setS] = useState({ items: [1, 2, 3], mid: false });
      set = setS;
      return E(
        Stage,
        { width: 10, height: 10 },
        E(
          KLayer,
          null,
          E(Rect, { name: 'head' }),
          s.mid ? E(Group, { name: 'mid' }) : null,
          s.items.map((i) => E(Rect, { key: i, name: String(i) })),
          E(Rect, { name: 'tail' }),
        ),
      );
    }
    const stage = Konva_stage(mount(E(Scene)));
    const layer = stage.children[0] as Konva.Layer;
    expect(names(layer)).toEqual(['head', '1', '2', '3', 'tail']);
    const two = layer.children[2];
    act(() => set({ items: [3, 2, 4], mid: true }));
    expect(names(layer)).toEqual(['head', 'mid', '3', '2', '4', 'tail']);
    expect(layer.children[3]).toBe(two);
    expect(layer.children.map((n) => n.index)).toEqual([0, 1, 2, 3, 4, 5]);
    act(() => set({ items: [4, 1], mid: false }));
    expect(names(layer)).toEqual(['head', '4', '1', 'tail']);
  });

  it('destroys removed nodes and releases their listeners', () => {
    let set!: (v: boolean) => void;
    const click = vi.fn();
    function Scene() {
      const [show, setShow] = useState(true);
      set = setShow;
      return E(Stage, { width: 10, height: 10 }, E(KLayer, null, show ? E(Group, { name: 'g' }, E(Circle, { name: 'c', onClick: click })) : null));
    }
    const stage = Konva_stage(mount(E(Scene)));
    const circle = stage.findOne('.c')!;
    const destroyed = vi.fn();
    stage.findOne('.g')!.on('destroy', destroyed);
    act(() => set(false));
    expect(stage.findOne('.g')).toBeUndefined();
    expect(destroyed).toHaveBeenCalledTimes(1);
    circle.fire('click', { evt: {} });
    expect(click).not.toHaveBeenCalled();
  });

  it('passes Konva nodes to object, callback and forwarded refs, and clears them', () => {
    const obj = { current: null as Konva.Rect | null };
    const fn = vi.fn();
    const fwd = { current: null as Konva.Line | null };
    const Forwarded = forwardRef<Konva.Line, { name: string }>((props, ref) => E(Line, { ...props, ref, points: [0, 0, 1, 1] }));
    let set!: (v: boolean) => void;
    let seenInLayout: Konva.Rect | null = null;
    function Scene() {
      const [show, setShow] = useState(true);
      set = setShow;
      useLayoutEffect(() => {
        seenInLayout = obj.current;
      }, []);
      return E(Stage, { width: 10, height: 10 }, E(KLayer, null, show ? [E(Rect, { key: 'r', ref: obj }), E(Text, { key: 't', ref: fn, text: 'hi' }), E(Forwarded, { key: 'f', name: 'fl', ref: fwd })] : null));
    }
    mount(E(Scene));
    expect(obj.current?.getClassName()).toBe('Rect');
    expect(obj.current?.getStage()?.getClassName()).toBe('Stage');
    // The parent's layout effects see the committed nodes (react-konva flushes sync).
    expect(seenInLayout).toBe(obj.current);
    expect(fn).toHaveBeenCalledWith(expect.objectContaining({ className: 'Text' }));
    expect(fwd.current?.getClassName()).toBe('Line');
    act(() => set(false));
    expect(obj.current).toBeNull();
    expect(fn).toHaveBeenLastCalledWith(null);
    expect(fwd.current).toBeNull();
  });

  it('attaches Transformer nodes from an effect (the canvas-overlay pattern)', () => {
    const boundBoxFunc = vi.fn((oldBox: unknown, newBox: unknown) => newBox);
    let tr!: Konva.Transformer;
    function Overlay() {
      const stageRef = useRef<Konva.Stage>(null);
      const trRef = useRef<Konva.Transformer>(null);
      useEffect(() => {
        const node = stageRef.current!.findOne('#item-a')!;
        trRef.current!.nodes([node]);
        trRef.current!.getLayer()?.batchDraw();
        tr = trRef.current!;
      }, []);
      return E(
        Stage,
        { ref: stageRef, width: 100, height: 100 },
        E(KLayer, null, E(Rect, { id: 'item-a', name: 'item', x: 10, y: 10, width: 20, height: 20, draggable: true, onDragMove: () => {} }), E(Transformer, { ref: trRef, rotateEnabled: true, keepRatio: false, ignoreStroke: true, anchorSize: 10, flipEnabled: false, boundBoxFunc })),
      );
    }
    mount(E(Overlay));
    expect(tr.nodes().map((n) => n.id())).toEqual(['item-a']);
    expect(tr.boundBoxFunc()).toBe(boundBoxFunc);
    expect(tr.keepRatio()).toBe(false);
    expect(tr.ignoreStroke()).toBe(true);
    expect(tr.anchorSize()).toBe(10);
  });

  it('runs hover state through Konva events and reads pointer positions', () => {
    let pos: { x: number; y: number } | null = null;
    function Item() {
      const [hover, setHover] = useState(false);
      return E(Rect, {
        name: 'item',
        fill: hover ? 'blue' : 'gray',
        onMouseEnter: (e: Konva.KonvaEventObject<MouseEvent>) => {
          setHover(true);
          const c = e.target.getStage()?.container();
          if (c) c.style.cursor = 'move';
        },
        onMouseLeave: () => setHover(false),
        onMouseDown: (e: Konva.KonvaEventObject<MouseEvent>) => {
          const stage = e.target.getStage()!;
          stage.setPointersPositions({ clientX: 30, clientY: 50 } as unknown as MouseEvent);
          pos = stage.getRelativePointerPosition();
        },
      });
    }
    const stage = Konva_stage(mount(E(Stage, { width: 100, height: 100, scaleX: 2, scaleY: 2 }, E(KLayer, null, E(Item)))));
    const rect = stage.findOne('.item') as Konva.Rect;
    act(() => {
      rect.fire('mouseenter', { evt: {} });
    });
    expect(rect.fill()).toBe('blue');
    expect(stage.container().style.cursor).toBe('move');
    act(() => {
      rect.fire('mouseleave', { evt: {} });
    });
    expect(rect.fill()).toBe('gray');
    rect.fire('mousedown', { evt: { button: 0 } }, true);
    expect(pos).toEqual({ x: 15, y: 25 });
  });

  it('passes context through the Stage, also with useContextBridge', () => {
    const Theme = createContext('default');
    let set!: (v: string) => void;
    function Inner() {
      return E(Rect, { name: 'ctx:' + useContext(Theme) });
    }
    function Outer() {
      const Bridge = useContextBridge();
      return E(Stage, { width: 10, height: 10 }, E(Bridge, null, E(KLayer, null, E(Inner))));
    }
    function App() {
      const [v, setV] = useState('red');
      set = setV;
      return E(Theme.Provider, { value: v }, E(Outer));
    }
    const stage = Konva_stage(mount(E(App)));
    expect(names(stage.children[0] as Konva.Layer)).toEqual(['ctx:red']);
    act(() => set('blue'));
    expect(names(stage.children[0] as Konva.Layer)).toEqual(['ctx:blue']);
  });

  it('resets dragged attributes in strict mode', () => {
    let set!: (v: string) => void;
    function Scene() {
      const [fill, setFill] = useState('red');
      set = setFill;
      return E(Stage, { width: 10, height: 10 }, E(KLayer, null, E(Rect, { name: 'r', x: 10, fill })));
    }
    const stage = Konva_stage(mount(E(Scene)));
    const rect = stage.findOne('.r') as Konva.Rect;
    rect.x(50);
    act(() => set('blue'));
    expect(rect.x()).toBe(50);
    useStrictMode(true);
    act(() => set('green'));
    expect(rect.x()).toBe(10);
  });

  it('tears down the stage on unmount', () => {
    const ref = { current: null as Konva.Stage | null };
    const rectRef = { current: null as Konva.Rect | null };
    const el = mount(E(Stage, { ref, width: 10, height: 10 }, E(KLayer, null, E(Rect, { ref: rectRef }))));
    const stage = ref.current!;
    const div = el.firstChild as HTMLDivElement;
    expect(div.querySelector('.konvajs-content')).not.toBeNull();
    act(() => render(null, el));
    expect(ref.current).toBeNull();
    expect(rectRef.current).toBeNull();
    expect(stage.children).toHaveLength(0);
    expect(div.querySelector('.konvajs-content')).toBeNull();
  });
});

// The Konva.Stage of a mounted island (Konva keeps a list of stages).
function Konva_stage(el: HTMLElement): Konva.Stage {
  const div = el.querySelector('div')!;
  return engine.Konva.stages.find((s) => s.container() === div)!;
}
