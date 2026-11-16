// Pointer events through raycasting. Ported from @react-three/fiber (MIT,
// Copyright (c) 2019-2025 Poimandres), see NOTICE.

import * as THREE from 'three';
import type { Instance } from './instance.js';
import type { RootState, RootStore } from './store.js';
import { getRootState, type Camera, type Properties } from './utils.js';

export interface Intersection extends THREE.Intersection {
  /** The event source (the object which registered the handler) */
  eventObject: THREE.Object3D;
}

export interface IntersectionEvent<TSourceEvent> extends Intersection {
  /** The event source (the object which registered the handler) */
  eventObject: THREE.Object3D;
  /** An array of intersections */
  intersections: Intersection[];
  /** vec3.set(pointer.x, pointer.y, 0).unproject(camera) */
  unprojectedPoint: THREE.Vector3;
  /** Normalized event coordinates */
  pointer: THREE.Vector2;
  /** Delta between first click and this event */
  delta: number;
  /** The ray that pierced it */
  ray: THREE.Ray;
  /** The camera that was used by the raycaster */
  camera: Camera;
  /** stopPropagation will stop underlying handlers from firing */
  stopPropagation: () => void;
  /** The original host event */
  nativeEvent: TSourceEvent;
  /** If the event was stopped by calling stopPropagation */
  stopped: boolean;
}

export type ThreeEvent<TEvent> = IntersectionEvent<TEvent> & Properties<TEvent>;
export type DomEvent = PointerEvent | MouseEvent | WheelEvent;

export interface Events {
  onClick: EventListener;
  onContextMenu: EventListener;
  onDoubleClick: EventListener;
  onWheel: EventListener;
  onPointerDown: EventListener;
  onPointerUp: EventListener;
  onPointerLeave: EventListener;
  onPointerMove: EventListener;
  onPointerCancel: EventListener;
  onLostPointerCapture: EventListener;
}

export interface EventHandlers {
  onClick?: (event: ThreeEvent<MouseEvent>) => void;
  onContextMenu?: (event: ThreeEvent<MouseEvent>) => void;
  onDoubleClick?: (event: ThreeEvent<MouseEvent>) => void;
  onPointerUp?: (event: ThreeEvent<PointerEvent>) => void;
  onPointerDown?: (event: ThreeEvent<PointerEvent>) => void;
  onPointerOver?: (event: ThreeEvent<PointerEvent>) => void;
  onPointerOut?: (event: ThreeEvent<PointerEvent>) => void;
  onPointerEnter?: (event: ThreeEvent<PointerEvent>) => void;
  onPointerLeave?: (event: ThreeEvent<PointerEvent>) => void;
  onPointerMove?: (event: ThreeEvent<PointerEvent>) => void;
  onPointerMissed?: (event: MouseEvent) => void;
  onPointerCancel?: (event: ThreeEvent<PointerEvent>) => void;
  onWheel?: (event: ThreeEvent<WheelEvent>) => void;
  onLostPointerCapture?: (event: ThreeEvent<PointerEvent>) => void;
}

export type FilterFunction = (items: THREE.Intersection[], state: RootState) => THREE.Intersection[];
export type ComputeFunction = (event: DomEvent, root: RootState, previous?: RootState) => void;

export interface EventManager<TTarget> {
  /** Determines if the event layer is active */
  enabled: boolean;
  /** Event layer priority, higher prioritized layers come first and may stop(-propagate) lower layer */
  priority: number;
  /** The compute function needs to set up the raycaster and an xy- pointer */
  compute?: ComputeFunction;
  /** The filter can re-order or re-structure the intersections */
  filter?: FilterFunction;
  /** The target node the event layer is tied to */
  connected?: TTarget;
  /** All the pointer event handlers through which the host forwards native events */
  handlers?: Events;
  /** Allows re-connecting to another target */
  connect?: (target: TTarget) => void;
  /** Removes all existing events handlers from the target */
  disconnect?: () => void;
  /** Triggers a onPointerMove with the last known event */
  update?: () => void;
}

export interface PointerCaptureTarget {
  intersection: Intersection;
  target: Element;
}

function makeId(event: Partial<THREE.Intersection> & { eventObject?: THREE.Object3D }): string {
  return (event.eventObject || event.object)!.uuid + '/' + event.index + event.instanceId;
}

// Releases a pointer capture (releasePointerCapture, or an object removal).
function releaseInternalPointerCapture(
  capturedMap: Map<number, Map<THREE.Object3D, PointerCaptureTarget>>,
  obj: THREE.Object3D,
  captures: Map<THREE.Object3D, PointerCaptureTarget>,
  pointerId: number,
): void {
  const captureData = captures.get(obj);
  if (captureData) {
    captures.delete(obj);
    if (captures.size === 0) {
      capturedMap.delete(pointerId);
      captureData.target.releasePointerCapture(pointerId);
    }
  }
}

/** Moves all interactivity state from one object to another (instance reconstruction). */
export function swapInteractivity(store: RootStore, object: THREE.Object3D, newObject: THREE.Object3D): void {
  const { internal } = store.getState();
  for (let i = 0; i < internal.interaction.length; i++) {
    if (internal.interaction[i] === object) internal.interaction[i] = newObject;
  }
  for (let i = 0; i < internal.initialHits.length; i++) {
    if (internal.initialHits[i] === object) internal.initialHits[i] = newObject;
  }
  internal.hovered.forEach((value, key) => {
    if (value.eventObject === object || value.object === object) {
      internal.hovered.delete(key);
      const next = {
        ...value,
        eventObject: value.eventObject === object ? newObject : value.eventObject,
        object: value.object === object ? newObject : value.object,
      };
      internal.hovered.set(makeId(next), next);
    }
  });
  internal.capturedMap.forEach((captures) => {
    for (const [key, captureData] of Array.from(captures)) {
      const { intersection } = captureData;
      if (key !== object && intersection.object !== object && intersection.eventObject !== object) continue;
      if (key === object) captures.delete(key);
      captures.set(key === object ? newObject : key, {
        ...captureData,
        intersection: {
          ...intersection,
          object: intersection.object === object ? newObject : intersection.object,
          eventObject: intersection.eventObject === object ? newObject : intersection.eventObject,
        },
      });
    }
  });
}

/** Removes every trace of an object from the event state. */
export function removeInteractivity(store: RootStore, object: THREE.Object3D): void {
  const { internal } = store.getState();
  internal.interaction = internal.interaction.filter((o) => o !== object);
  internal.initialHits = internal.initialHits.filter((o) => o !== object);
  internal.hovered.forEach((value, key) => {
    if (value.eventObject === object || value.object === object) internal.hovered.delete(key);
  });
  internal.capturedMap.forEach((captures, pointerId) => {
    releaseInternalPointerCapture(internal.capturedMap, object, captures, pointerId);
  });
}

type R3F = { __r3f?: Instance };

/** Creates the pointer handlers of a root: raycasting, bubbling, hover and capture. */
export function createEvents(store: RootStore): { handlePointer: (name: string) => (event: DomEvent) => void } {
  function calculateDistance(event: DomEvent): number {
    const { internal } = store.getState();
    const dx = event.offsetX - internal.initialClick[0];
    const dy = event.offsetY - internal.initialClick[1];
    return Math.round(Math.sqrt(dx * dx + dy * dy));
  }

  // Objects with a hover or move handler (pointer move only raycasts those).
  function filterPointerEvents(objects: THREE.Object3D[]): THREE.Object3D[] {
    return objects.filter((obj) =>
      ['Move', 'Over', 'Enter', 'Out', 'Leave'].some(
        (name) => ((obj as R3F).__r3f?.handlers as Record<string, unknown> | undefined)?.['onPointer' + name],
      ),
    );
  }

  function intersect(event: DomEvent, filter?: (objects: THREE.Object3D[]) => THREE.Object3D[]): Intersection[] {
    const state = store.getState();
    const duplicates = new Set<string>();
    const intersections: Intersection[] = [];
    const eventsObjects = filter ? filter(state.internal.interaction) : state.internal.interaction;
    // Reset the raycaster cameras so every layer computes its ray once.
    for (let i = 0; i < eventsObjects.length; i++) {
      const s = getRootState(eventsObjects[i]);
      if (s) s.raycaster.camera = undefined as unknown as THREE.Camera;
    }
    if (!state.previousRoot) state.events.compute?.(event, state);

    function handleRaycast(obj: THREE.Object3D): THREE.Intersection[] {
      const s = getRootState(obj);
      if (!s || !s.events.enabled || s.raycaster.camera === null) return [];
      if (s.raycaster.camera === undefined) {
        s.events.compute?.(event, s, s.previousRoot?.getState());
        if (s.raycaster.camera === undefined) s.raycaster.camera = null as unknown as THREE.Camera;
      }
      return s.raycaster.camera ? s.raycaster.intersectObject(obj, true) : [];
    }

    let hits: THREE.Intersection[] = eventsObjects
      .flatMap(handleRaycast)
      .sort((a, b) => {
        const aState = getRootState(a.object);
        const bState = getRootState(b.object);
        if (!aState || !bState) return a.distance - b.distance;
        return bState.events.priority - aState.events.priority || a.distance - b.distance;
      })
      .filter((item) => {
        const id = makeId(item as Intersection);
        if (duplicates.has(id)) return false;
        duplicates.add(id);
        return true;
      });

    // https://github.com/mrdoob/three.js/issues/16031
    if (state.events.filter) hits = state.events.filter(hits, state);

    // Bubble up to every ancestor that has handlers.
    for (const hit of hits) {
      let eventObject: THREE.Object3D | null = hit.object;
      while (eventObject) {
        if ((eventObject as R3F).__r3f?.eventCount) intersections.push({ ...hit, eventObject });
        eventObject = eventObject.parent;
      }
    }

    // Captured pointers deliver to their capturing objects too.
    if ('pointerId' in event && state.internal.capturedMap.has(event.pointerId)) {
      for (const captureData of state.internal.capturedMap.get(event.pointerId)!.values()) {
        if (!duplicates.has(makeId(captureData.intersection))) intersections.push(captureData.intersection);
      }
    }
    return intersections;
  }

  function handleIntersects(
    intersections: Intersection[],
    event: DomEvent,
    delta: number,
    callback: (event: ThreeEvent<DomEvent>) => void,
  ): Intersection[] {
    if (intersections.length) {
      const localState = { stopped: false };
      for (const hit of intersections) {
        let state = getRootState(hit.object);
        // An object not managed here may sit below one that is.
        if (!state) {
          hit.object.traverseAncestors((obj) => {
            const parentState = getRootState(obj);
            if (parentState) {
              state = parentState;
              return false;
            }
          });
        }
        if (state) {
          const { raycaster, pointer, camera, internal } = state;
          const unprojectedPoint = new THREE.Vector3(pointer.x, pointer.y, 0).unproject(camera);
          const hasPointerCapture = (id: number): boolean => internal.capturedMap.get(id)?.has(hit.eventObject) ?? false;
          const setPointerCapture = (id: number): void => {
            const captureData = { intersection: hit, target: event.target as Element };
            if (internal.capturedMap.has(id)) internal.capturedMap.get(id)!.set(hit.eventObject, captureData);
            else internal.capturedMap.set(id, new Map([[hit.eventObject, captureData]]));
            (event.target as Element).setPointerCapture(id);
          };
          const releasePointerCapture = (id: number): void => {
            const captures = internal.capturedMap.get(id);
            if (captures) releaseInternalPointerCapture(internal.capturedMap, hit.eventObject, captures, id);
          };

          // Copy the native event's own and inherited non-function properties.
          const extractEventProps: Record<string, unknown> = {};
          for (const prop in event) {
            const property = (event as unknown as Record<string, unknown>)[prop];
            if (typeof property !== 'function') extractEventProps[prop] = property;
          }

          const raycastEvent: any = {
            ...hit,
            ...extractEventProps,
            pointer,
            intersections,
            stopped: localState.stopped,
            delta,
            unprojectedPoint,
            ray: raycaster.ray,
            camera,
            stopPropagation() {
              // A captured pointer may only be stopped by a capturing object.
              const capturesForPointer = 'pointerId' in event && internal.capturedMap.get(event.pointerId);
              if (!capturesForPointer || capturesForPointer.has(hit.eventObject)) {
                raycastEvent.stopped = localState.stopped = true;
                // A hovered object that stops propagation unhovers the ones below it.
                if (internal.hovered.size && Array.from(internal.hovered.values()).find((i) => i.eventObject === hit.eventObject)) {
                  const higher = intersections.slice(0, intersections.indexOf(hit));
                  cancelPointer([...higher, hit]);
                }
              }
            },
            target: { hasPointerCapture, setPointerCapture, releasePointerCapture },
            currentTarget: { hasPointerCapture, setPointerCapture, releasePointerCapture },
            nativeEvent: event,
          };
          callback(raycastEvent);
          if (localState.stopped === true) break;
        }
      }
    }
    return intersections;
  }

  function cancelPointer(intersections: Intersection[]): void {
    const { internal } = store.getState();
    for (const hoveredObj of internal.hovered.values()) {
      if (
        !intersections.length ||
        !intersections.find(
          (hit) => hit.object === hoveredObj.object && hit.index === hoveredObj.index && hit.instanceId === hoveredObj.instanceId,
        )
      ) {
        const eventObject = hoveredObj.eventObject;
        const instance = (eventObject as R3F).__r3f;
        internal.hovered.delete(makeId(hoveredObj));
        if (instance?.eventCount) {
          const handlers = instance.handlers;
          const data = { ...hoveredObj, intersections } as ThreeEvent<PointerEvent>;
          handlers.onPointerOut?.(data);
          handlers.onPointerLeave?.(data);
        }
      }
    }
  }

  function pointerMissed(event: MouseEvent, objects: THREE.Object3D[]): void {
    for (let i = 0; i < objects.length; i++) {
      (objects[i] as R3F).__r3f?.handlers.onPointerMissed?.(event);
    }
  }

  function handlePointer(name: string): (event: DomEvent) => void {
    switch (name) {
      case 'onPointerLeave':
      case 'onPointerCancel':
        return () => cancelPointer([]);
      case 'onLostPointerCapture':
        return (event: DomEvent) => {
          const { internal } = store.getState();
          if ('pointerId' in event && internal.capturedMap.has(event.pointerId)) {
            // Lost capture fires before pointerup; release on the next frame so
            // pointerup still reaches the capturing object.
            requestAnimationFrame(() => {
              if (internal.capturedMap.has(event.pointerId)) {
                internal.capturedMap.delete(event.pointerId);
                cancelPointer([]);
              }
            });
          }
        };
    }

    return function handleEvent(event: DomEvent): void {
      const { onPointerMissed, internal } = store.getState();
      internal.lastEvent.current = event;

      const isPointerMove = name === 'onPointerMove';
      const isClickEvent = name === 'onClick' || name === 'onContextMenu' || name === 'onDoubleClick';
      const filter = isPointerMove ? filterPointerEvents : undefined;
      const hits = intersect(event, filter);
      const delta = isClickEvent ? calculateDistance(event) : 0;

      if (name === 'onPointerDown') {
        internal.initialClick = [event.offsetX, event.offsetY];
        internal.initialHits = hits.map((hit) => hit.eventObject);
      }

      // A click that hits nothing is a miss; misses run first.
      if (isClickEvent && !hits.length) {
        if (delta <= 2) {
          pointerMissed(event, internal.interaction);
          if (onPointerMissed) onPointerMissed(event);
        }
      }
      if (isPointerMove) cancelPointer(hits);

      function onIntersect(data: ThreeEvent<DomEvent>): void {
        const eventObject = data.eventObject;
        const instance = (eventObject as R3F).__r3f;
        if (!instance?.eventCount) return;
        const handlers = instance.handlers as Record<string, ((e: unknown) => void) | undefined>;

        if (isPointerMove) {
          if (handlers.onPointerOver || handlers.onPointerEnter || handlers.onPointerOut || handlers.onPointerLeave) {
            const id = makeId(data);
            const hoveredItem = internal.hovered.get(id);
            if (!hoveredItem) {
              internal.hovered.set(id, data);
              handlers.onPointerOver?.(data);
              handlers.onPointerEnter?.(data);
            } else if (hoveredItem.stopped) {
              data.stopPropagation();
            }
          }
          handlers.onPointerMove?.(data);
        } else {
          const handler = handlers[name];
          if (handler) {
            // Clicks only reach objects that were hit on pointer down.
            if (!isClickEvent || internal.initialHits.includes(eventObject)) {
              pointerMissed(event, internal.interaction.filter((object) => !internal.initialHits.includes(object)));
              handler(data);
            }
          } else if (isClickEvent && internal.initialHits.includes(eventObject)) {
            pointerMissed(event, internal.interaction.filter((object) => !internal.initialHits.includes(object)));
          }
        }
      }
      handleIntersects(hits, event, delta, onIntersect);
    };
  }

  return { handlePointer };
}
