import type Konva from 'konva';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { EVENTS_NAMESPACE, mountStage, toggleStrictMode } from './index.js';

// Compiled stage code calls the global `h`; the engine installs its own while
// it evaluates the children. The react-konva elements are strings used as
// components, so reactive props are getters and function props are values.
const h = (tag: unknown, props: Record<string, unknown> | null, ...children: unknown[]): unknown =>
  (globalThis as unknown as { h: (...a: unknown[]) => unknown }).h(tag, props, ...children);
const g = globalThis as unknown as {
  createSignal<T>(v: T): [() => T, (v: T) => void];
  krateFlushSync(): void;
  onCleanup(fn: () => void): void;
  createContext(v: string): { useContextLive(): () => string; Provider: unknown };
  krateContextRows(entries: unknown[], fn: () => unknown): () => unknown;
};

// The object a compiled `.map()` produces (krateList), as a custom renderer sees it.
function list(items: () => unknown[], key: ((item: any, i: number) => unknown) | null, render: (item: any, i?: any) => unknown, mode = 1) {
  return { __ka: 1, items, key, render, mode };
}

const teardowns: Array<() => void> = [];
afterEach(() => {
  for (const t of teardowns.splice(0)) t();
  toggleStrictMode(false);
});

function mount(children: unknown, stageProps: Record<string, unknown> = {}) {
  const div = document.createElement('div');
  document.body.appendChild(div);
  let stage!: Konva.Stage;
  const props = { width: 200, height: 100, ref: (s: Konva.Stage) => (stage = s), children, ...stageProps };
  const teardown = mountStage(div, () => props);
  teardowns.push(teardown);
  const layer = (): Konva.Layer => stage.children[0] as Konva.Layer;
  return { div, stage, layer, teardown };
}

async function flush(): Promise<void> {
  g.krateFlushSync();
  await new Promise<void>((r) => setTimeout(r, 0));
}

const names = (c: Konva.Container): string[] => c.children.map((n) => n.name());

describe('nodes', () => {
  it('creates the stage, layers and shapes with their props', () => {
    const { div, stage, layer } = mount(() => [
      h('Layer', null, h('Rect', { name: 'r', x: 10, y: 20, width: 30, height: 40, fill: 'red', dash: [4, 2], listening: false, hitStrokeWidth: 8, shadowColor: '#000', shadowBlur: 3, shadowOpacity: 0.5 })),
    ]);
    expect(stage.width()).toBe(200);
    expect(stage.container()).toBe(div);
    expect(div.querySelector('.konvajs-content')).not.toBeNull();
    const rect = layer().children[0] as Konva.Rect;
    expect(rect.getClassName()).toBe('Rect');
    expect(rect.x()).toBe(10);
    expect(rect.fill()).toBe('red');
    expect(rect.dash()).toEqual([4, 2]);
    expect(rect.listening()).toBe(false);
    expect(rect.hitStrokeWidth()).toBe(8);
    expect(rect.shadowColor()).toBe('#000');
    expect(rect.shadowBlur()).toBe(3);
    expect(rect.shadowOpacity()).toBe(0.5);
  });

  it('creates every react-konva element type', () => {
    const types = ['Group', 'Label', 'Rect', 'Circle', 'Ellipse', 'Wedge', 'Line', 'Sprite', 'Image', 'Text', 'TextPath', 'Star', 'Ring', 'Arc', 'Tag', 'Path', 'RegularPolygon', 'Arrow', 'Shape', 'Transformer'];
    const { stage, layer } = mount(() => [h('Layer', null, ...types.map((t) => h(t, { name: t }))), h('FastLayer', null)]);
    expect(layer().children.map((n) => n.getClassName())).toEqual(types);
    expect(stage.children[1].getClassName()).toBe('FastLayer');
  });

  it('updates reactive props in place and sets undefined values', async () => {
    const [x, setX] = g.createSignal(1);
    const [fill, setFill] = g.createSignal<string | undefined>('red');
    const { layer } = mount(() => [
      h('Layer', null, h('Rect', { get x() { return x(); }, get fill() { return fill(); }, width: 5 })),
    ]);
    const rect = layer().children[0] as Konva.Rect;
    setX(42);
    setFill(undefined);
    await flush();
    expect(layer().children[0]).toBe(rect);
    expect(rect.x()).toBe(42);
    expect(rect.fill()).toBeUndefined();
    expect(rect.width()).toBe(5);
  });

  it('passes function props as values (dragBoundFunc, sceneFunc)', () => {
    const scene = vi.fn((ctx: Konva.Context, shape: Konva.Shape) => {
      ctx.beginPath();
      ctx.fillStrokeShape(shape);
    });
    const bound = (pos: { x: number; y: number }) => ({ x: pos.x, y: 0 });
    const { layer } = mount(() => [h('Layer', null, h('Shape', { sceneFunc: scene, draggable: true, dragBoundFunc: bound, onDragMove: () => {} }))]);
    const shape = layer().children[0] as Konva.Shape;
    expect(shape.dragBoundFunc()).toBe(bound);
    layer().draw();
    expect(scene).toHaveBeenCalled();
  });

  it('warns about text children', () => {
    const err = vi.spyOn(console, 'error').mockImplementation(() => {});
    mount(() => [h('Layer', null, 'hello')]);
    expect(err).toHaveBeenCalledWith(expect.stringContaining('hello'));
    err.mockRestore();
  });
});

describe('events', () => {
  it('binds handlers in the react-konva namespace after attributes', () => {
    const click = vi.fn();
    const enter = vi.fn();
    const { layer } = mount(() => [h('Layer', null, h('Circle', { radius: 4, draggable: true, onClick: click, onMouseEnter: enter, onDragEnd: () => {} }))]);
    const circle = layer().children[0] as Konva.Circle;
    circle.fire('click', { evt: {} });
    circle.fire('mouseenter', { evt: {} });
    expect(click).toHaveBeenCalledTimes(1);
    expect(enter).toHaveBeenCalledTimes(1);
    const listeners = (circle as unknown as { eventListeners: Record<string, Array<{ name: string }>> }).eventListeners;
    expect(listeners.click.map((l) => l.name)).toEqual(['react-konva-event']);
    // draggable added Konva's own mousedown listener; ours comes after it.
    expect(listeners.dragend.map((l) => l.name)).toEqual(['react-konva-event']);
  });

  it('rebinds a handler only when it changes, and unbinds a removed one', async () => {
    const a = vi.fn();
    const b = vi.fn();
    const [handler, setHandler] = g.createSignal<((e: unknown) => void) | undefined>(a);
    const [n, setN] = g.createSignal(0);
    const { layer } = mount(() => [h('Layer', null, h('Rect', { get onDblClick() { return handler(); }, get x() { return n(); } }))]);
    const rect = layer().children[0] as Konva.Rect;
    const on = vi.spyOn(rect, 'on');
    setN(1);
    await flush();
    expect(on).not.toHaveBeenCalled();
    setHandler(b);
    await flush();
    rect.fire('dblclick', { evt: {} });
    expect(a).not.toHaveBeenCalled();
    expect(b).toHaveBeenCalledTimes(1);
    setHandler(undefined);
    await flush();
    rect.fire('dblclick', { evt: {} });
    expect(b).toHaveBeenCalledTimes(1);
  });

  it('maps every event prop name to its Konva event', () => {
    const fired: string[] = [];
    const events = ['onPointerDown', 'onPointerMove', 'onWheel', 'onTap', 'onDblTap', 'onDragStart', 'onTransformStart', 'onTransform', 'onTransformEnd', 'onMouseLeave'];
    const props: Record<string, unknown> = {};
    for (const e of events) props[e] = () => fired.push(e);
    const { layer } = mount(() => [h('Layer', null, h('Rect', props))]);
    const rect = layer().children[0];
    for (const e of ['pointerdown', 'pointermove', 'wheel', 'tap', 'dbltap', 'dragstart', 'transformstart', 'transform', 'transformend', 'mouseleave']) rect.fire(e, { evt: {} });
    expect(fired).toEqual(events);
  });

  it('gives handlers the target and its stage', () => {
    let seen: Konva.Stage | null = null;
    const { stage, layer } = mount(() => [h('Layer', null, h('Rect', { onClick: (e: Konva.KonvaEventObject<MouseEvent>) => (seen = e.target.getStage()) }))]);
    layer().children[0].fire('click', { evt: {} }, true);
    expect(seen).toBe(stage);
  });
});

describe('regions and lists', () => {
  it('mounts and unmounts conditional content in order', async () => {
    const [show, setShow] = g.createSignal(false);
    const { layer } = mount(() => [h('Layer', null, h('Rect', { name: 'first' }), () => show() && h('Rect', { name: 'middle' }), h('Rect', { name: 'last' }))]);
    expect(names(layer())).toEqual(['first', 'last']);
    setShow(true);
    await flush();
    expect(names(layer())).toEqual(['first', 'middle', 'last']);
    const middle = layer().children[1];
    const destroyed = vi.fn();
    (middle as Konva.Node).on('destroy', destroyed);
    setShow(false);
    await flush();
    expect(names(layer())).toEqual(['first', 'last']);
    expect(destroyed).toHaveBeenCalled();
  });

  it('reconciles keyed lists: keeps, adds, removes and reorders', async () => {
    const [items, setItems] = g.createSignal([1, 2, 3]);
    const { layer } = mount(() => [
      h('Layer', null, h('Rect', { name: 'head' }), list(items, (i) => i, (item: () => number) => h('Rect', { get name() { return String(item()); } })), h('Rect', { name: 'tail' })),
    ]);
    expect(names(layer())).toEqual(['head', '1', '2', '3', 'tail']);
    const two = layer().children[2];
    setItems([3, 2, 4]);
    await flush();
    expect(names(layer())).toEqual(['head', '3', '2', '4', 'tail']);
    expect(layer().children[2]).toBe(two);
    expect(layer().children.map((n) => n.index)).toEqual([0, 1, 2, 3, 4]);
    setItems([4, 1]);
    await flush();
    expect(names(layer())).toEqual(['head', '4', '1', 'tail']);
    setItems([]);
    await flush();
    expect(names(layer())).toEqual(['head', 'tail']);
  });

  it('nests components, regions and lists in one parent in order', async () => {
    const [show, setShow] = g.createSignal(false);
    const [items, setItems] = g.createSignal(['x']);
    function Pair() {
      return [h('Rect', { name: 'p1' }), h('Rect', { name: 'p2' })];
    }
    const { layer } = mount(() => [
      h('Layer', null, h('Rect', { name: 'a' }), h(Pair, null), () => show() && h('Group', { name: 'cond' }, h('Rect', { name: 'inner' })), list(items, (i) => i, (i: () => string) => h('Rect', { get name() { return 'row-' + i(); } })), h('Rect', { name: 'z' })),
    ]);
    expect(names(layer()).join(",")).toBe('a,p1,p2,row-x,z');
    setShow(true);
    setItems(['y', 'x']);
    await flush();
    expect(names(layer()).join(",")).toBe('a,p1,p2,cond,row-y,row-x,z');
    expect(names(layer().children[3] as Konva.Group)).toEqual(['inner']);
  });

  it('orders layers in the stage and their canvases in the DOM', async () => {
    const [show, setShow] = g.createSignal(false);
    const { stage, div } = mount(() => [h('Layer', { name: 'bottom' }), () => show() && h('Layer', { name: 'mid' }), h('Layer', { name: 'top' })]);
    setShow(true);
    await flush();
    expect(names(stage)).toEqual(['bottom', 'mid', 'top']);
    const canvases = Array.from(div.querySelectorAll('canvas'));
    expect(canvases).toEqual(stage.children.map((l) => (l as Konva.Layer).getNativeCanvasElement()));
  });
});

describe('refs', () => {
  it('sets object, callback and variable refs and clears them on unmount', async () => {
    const obj = { current: null as Konva.Rect | null };
    const fn = vi.fn();
    let variable: Konva.Circle | null | undefined;
    // The compiler lowers `ref={variable}` to a callback that assigns it.
    const assign = (v: Konva.Circle | null) => {
      variable = v;
    };
    const [show, setShow] = g.createSignal(true);
    const { stage } = mount(() => [h('Layer', null, () => show() && [h('Rect', { ref: obj }), h('Group', { ref: fn }), h('Circle', { ref: assign })])]);
    expect(obj.current?.getClassName()).toBe('Rect');
    // Refs are attached once the node is in the stage.
    expect(obj.current?.getStage()).toBe(stage);
    expect(fn).toHaveBeenCalledWith(expect.objectContaining({ nodeType: 'Group' }));
    expect(variable?.getClassName()).toBe('Circle');
    setShow(false);
    await flush();
    expect(obj.current).toBeNull();
    expect(fn).toHaveBeenLastCalledWith(null);
    expect(variable).toBeNull();
  });

  it('passes the stage to the Stage ref and applies reactive Stage props', async () => {
    const [scale, setScale] = g.createSignal(1);
    const ref = { current: null as Konva.Stage | null };
    const onMouseDown = vi.fn();
    const props: Record<string, unknown> = {
      width: 300,
      height: 150,
      ref,
      onMouseDown,
      get scaleX() {
        return scale();
      },
      children: () => [h('Layer', null)],
    };
    const div = document.createElement('div');
    teardowns.push(mountStage(div, () => props));
    const stage = ref.current!;
    expect(stage.width()).toBe(300);
    setScale(2);
    await flush();
    expect(stage.scaleX()).toBe(2);
    stage.fire('mousedown', { evt: {} });
    expect(onMouseDown).toHaveBeenCalled();
  });
});

describe('Transformer', () => {
  it('attaches nodes and keeps its options', () => {
    const tr = { current: null as Konva.Transformer | null };
    const rect = { current: null as Konva.Rect | null };
    const boundBoxFunc = (oldBox: unknown, newBox: { width: number }) => (newBox.width < 4 ? oldBox : newBox);
    mount(() => [
      h(
        'Layer',
        null,
        h('Rect', { ref: rect, x: 10, y: 10, width: 50, height: 20, draggable: true, onDragEnd: () => {} }),
        h('Transformer', { ref: tr, boundBoxFunc, keepRatio: false, rotateEnabled: true, anchorSize: 9, ignoreStroke: true, rotationSnaps: [0, 90] }),
      ),
    ]);
    const t = tr.current!;
    t.nodes([rect.current!]);
    expect(t.nodes()).toEqual([rect.current]);
    expect(t.boundBoxFunc()).toBe(boundBoxFunc);
    expect(t.keepRatio()).toBe(false);
    expect(t.anchorSize()).toBe(9);
    expect(t.ignoreStroke()).toBe(true);
    expect(t.rotationSnaps()).toEqual([0, 90]);
    expect(t.getLayer()).toBe(rect.current!.getLayer());
  });
});

describe('components and context', () => {
  it('runs components once and cleans them up on unmount', async () => {
    const body = vi.fn();
    const cleanup = vi.fn();
    function Handle(props: { x: number }) {
      body();
      g.onCleanup(cleanup);
      return h('Circle', { get x() { return props.x; }, radius: 3 });
    }
    const [x, setX] = g.createSignal(1);
    const [show, setShow] = g.createSignal(true);
    const { layer } = mount(() => [h('Layer', null, () => show() && h(Handle, { get x() { return x(); } }))]);
    expect((layer().children[0] as Konva.Circle).x()).toBe(1);
    setX(5);
    await flush();
    expect((layer().children[0] as Konva.Circle).x()).toBe(5);
    expect(body).toHaveBeenCalledTimes(1);
    setShow(false);
    await flush();
    expect(cleanup).toHaveBeenCalled();
    expect(layer().children).toHaveLength(0);
  });

  it('sees providers around the stage, live, and providers inside it', async () => {
    const Theme = g.createContext('default');
    const [color, setColor] = g.createSignal('red');
    function Inner() {
      const value = Theme.useContextLive();
      return h('Rect', { get name() { return 'ctx:' + value(); } });
    }
    const { layer } = mount(
      g.krateContextRows([[Theme, () => color()]], () => [h('Layer', null, h(Inner, null), h(Theme.Provider, { value: 'inside' }, h(Inner, null)))]),
    );
    expect(names(layer())).toEqual(['ctx:red', 'ctx:inside']);
    setColor('blue');
    await flush();
    expect(names(layer())).toEqual(['ctx:blue', 'ctx:inside']);
  });
});

describe('strict mode and teardown', () => {
  it('resets attributes changed outside the renderer only in strict mode', async () => {
    const [fill, setFill] = g.createSignal('red');
    const { layer } = mount(() => [h('Layer', null, h('Rect', { x: 10, get fill() { return fill(); } }))]);
    const rect = layer().children[0] as Konva.Rect;
    rect.x(99);
    setFill('blue');
    await flush();
    expect(rect.x()).toBe(99);
    toggleStrictMode(true);
    setFill('green');
    await flush();
    expect(rect.x()).toBe(10);
  });

  it('destroys the stage, its listeners and refs on teardown', () => {
    const ref = { current: null as Konva.Rect | null };
    const click = vi.fn();
    const { div, stage, teardown, layer } = mount(() => [h('Layer', null, h('Rect', { ref, onClick: click }))]);
    const rect = layer().children[0] as Konva.Rect;
    teardown();
    teardowns.splice(teardowns.indexOf(teardown), 1);
    expect(ref.current).toBeNull();
    expect(div.querySelector('.konvajs-content')).toBeNull();
    expect(stage.children).toHaveLength(0);
    rect.fire('click', { evt: {} });
    expect(click).not.toHaveBeenCalled();
    expect(EVENTS_NAMESPACE).toBe('.react-konva-event');
  });
});
