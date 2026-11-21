// Pointer picking: view.pick() with at most one query in flight, entity to
// handler dispatch, and over/out derived from the previously hovered entity.

import type { Pointer3DEvent, PointerHandler } from '../types.js';
import type { FPickResult, FView } from './filament.js';

export type PointerEventName = 'click' | 'pointerover' | 'pointerout' | 'pointermove' | 'pointerdown' | 'pointerup';
type Handlers = Partial<Record<PointerEventName, PointerHandler>>;

interface Request {
  type: 'click' | 'pointermove' | 'pointerdown' | 'pointerup';
  /** Pixels, bottom-left origin (Filament). */
  px: number;
  py: number;
  /** CSS pixels relative to the canvas, top-left origin. */
  x: number;
  y: number;
  native: PointerEvent | MouseEvent;
}

/** The real binding hands back an Entity object (0 or null for a miss), not the number its typings promise. */
function entityId(r: FPickResult['renderable']): number {
  if (typeof r === 'number') return r;
  return r?.getId() ?? 0;
}

const DOM_EVENTS = ['click', 'pointermove', 'pointerdown', 'pointerup'] as const;

export class Picker {
  private handlers = new Map<number, Handlers>();
  private queue: Request[] = [];
  private inflight = false;
  private hovered = 0;
  private attached = false;
  private disposed = false;

  constructor(
    private view: FView,
    private canvas: HTMLCanvasElement,
    private pixelRatio: () => number
  ) {}

  on(entity: number, event: PointerEventName, handler: PointerHandler): void {
    const h = this.handlers.get(entity) ?? {};
    h[event] = handler;
    this.handlers.set(entity, h);
    this.attach();
  }

  off(entity: number): void {
    this.handlers.delete(entity);
    if (this.hovered === entity) this.hovered = 0;
  }

  private attach(): void {
    if (this.attached) return;
    this.attached = true;
    for (const t of DOM_EVENTS) this.canvas.addEventListener(t, this.onPointer as EventListener);
    this.canvas.addEventListener('pointerleave', this.onLeave);
  }

  private onPointer = (e: PointerEvent | MouseEvent): void => {
    const rect = this.canvas.getBoundingClientRect();
    const dpr = this.pixelRatio();
    const x = e.clientX - rect.left;
    const y = e.clientY - rect.top;
    const req: Request = {
      type: e.type as Request['type'],
      px: x * dpr,
      py: this.canvas.height - y * dpr,
      x,
      y,
      native: e,
    };
    // Only the latest pointer position matters while waiting for a query.
    const last = this.queue[this.queue.length - 1];
    if (req.type === 'pointermove' && last?.type === 'pointermove') this.queue[this.queue.length - 1] = req;
    else this.queue.push(req);
  };

  private onLeave = (e: Event): void => {
    this.queue = this.queue.filter((r) => r.type !== 'pointermove');
    const prev = this.hovered;
    this.hovered = 0;
    if (prev) this.fire(prev, 'pointerout', 0, 0, e as PointerEvent);
  };

  /** Called once per frame before render: starts the next query unless one is in flight. */
  update(): void {
    if (this.inflight || this.disposed) return;
    const req = this.queue.shift();
    if (!req) return;
    this.inflight = true;
    this.view.pick(req.px, req.py, (r: FPickResult) => {
      this.inflight = false;
      if (!this.disposed) this.resolve(req, entityId(r.renderable));
    });
  }

  private resolve(req: Request, id: number): void {
    if (req.type === 'pointermove') {
      if (id !== this.hovered) {
        const prev = this.hovered;
        this.hovered = id;
        if (prev) this.fire(prev, 'pointerout', req.x, req.y, req.native);
        if (id) this.fire(id, 'pointerover', req.x, req.y, req.native);
      }
    }
    if (id) this.fire(id, req.type, req.x, req.y, req.native);
  }

  private fire(entity: number, name: PointerEventName, x: number, y: number, native: PointerEvent | MouseEvent): void {
    this.handlers.get(entity)?.[name]?.({ entity, x, y, nativeEvent: native } satisfies Pointer3DEvent);
  }

  dispose(): void {
    this.disposed = true;
    for (const t of DOM_EVENTS) this.canvas.removeEventListener(t, this.onPointer as EventListener);
    this.canvas.removeEventListener('pointerleave', this.onLeave);
    this.handlers.clear();
    this.queue = [];
  }
}
