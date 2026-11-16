// The default DOM event manager. Ported from @react-three/fiber (MIT,
// Copyright (c) 2019-2025 Poimandres), see NOTICE.

import { createEvents, type DomEvent, type EventManager, type Events } from './events.js';
import type { RootStore } from './store.js';

const DOM_EVENTS = {
  onClick: ['click', false],
  onContextMenu: ['contextmenu', false],
  onDoubleClick: ['dblclick', false],
  onWheel: ['wheel', true],
  onPointerDown: ['pointerdown', true],
  onPointerUp: ['pointerup', true],
  onPointerLeave: ['pointerleave', true],
  onPointerMove: ['pointermove', true],
  onPointerCancel: ['pointercancel', true],
  onLostPointerCapture: ['lostpointercapture', true],
} as const;

/** The default event manager: listens on the canvas container and raycasts from the pointer. */
export function createPointerEvents(store: RootStore): EventManager<HTMLElement> {
  const { handlePointer } = createEvents(store);
  return {
    priority: 1,
    enabled: true,
    compute(event: DomEvent, state) {
      state.pointer.set((event.offsetX / state.size.width) * 2 - 1, -(event.offsetY / state.size.height) * 2 + 1);
      state.raycaster.setFromCamera(state.pointer, state.camera);
    },
    connected: undefined,
    handlers: Object.keys(DOM_EVENTS).reduce(
      (acc, key) => ({ ...acc, [key]: handlePointer(key) }),
      {} as Events,
    ),
    update: () => {
      const { events, internal } = store.getState();
      if (internal.lastEvent?.current && events.handlers) events.handlers.onPointerMove(internal.lastEvent.current);
    },
    connect: (target: HTMLElement) => {
      const { set, events } = store.getState();
      events.disconnect?.();
      set((state) => ({ events: { ...state.events, connected: target } }));
      if (events.handlers) {
        for (const name in events.handlers) {
          const event = events.handlers[name as keyof Events];
          const [eventName, passive] = DOM_EVENTS[name as keyof typeof DOM_EVENTS];
          target.addEventListener(eventName, event, { passive });
        }
      }
    },
    disconnect: () => {
      const { set, events } = store.getState();
      if (events.connected) {
        if (events.handlers) {
          for (const name in events.handlers) {
            const event = events.handlers[name as keyof Events];
            const [eventName] = DOM_EVENTS[name as keyof typeof DOM_EVENTS];
            events.connected.removeEventListener(eventName, event);
          }
        }
        set((state) => ({ events: { ...state.events, connected: undefined } }));
      }
    },
  };
}
