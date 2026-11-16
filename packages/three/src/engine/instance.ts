// The descriptor @krate/three keeps on every object it manages (`object.__r3f`).

import type * as THREE from 'three';
import type { ConstructorRepresentation } from './extend.js';
import type { EventHandlers } from './events.js';
import type { RootStore } from './store.js';
import type { IsAllOptional } from './utils.js';

export type AttachFnType<O = any> = (parent: any, self: O) => () => void;
export type AttachType<O = any> = string | AttachFnType<O>;

export type Args<T> = T extends ConstructorRepresentation
  ? T extends typeof THREE.Color
    ? [r: number, g: number, b: number] | [color: THREE.ColorRepresentation]
    : ConstructorParameters<T>
  : any[];

type ArgsProp<P> = P extends ConstructorRepresentation
  ? IsAllOptional<ConstructorParameters<P>> extends true
    ? { args?: Args<P> }
    : { args: Args<P> }
  : { args: unknown[] };

export type InstanceProps<T = any, P = any> = ArgsProp<P> & {
  object?: T;
  dispose?: null;
  attach?: AttachType<T>;
  onUpdate?: (self: T) => void;
};

export interface Instance<O = any> {
  root: RootStore;
  type: string;
  parent: Instance | null;
  children: Instance[];
  props: InstanceProps<O> & Record<string, unknown>;
  object: O & { __r3f?: Instance<O> };
  eventCount: number;
  handlers: Partial<EventHandlers>;
  attach?: AttachType<O>;
  previousAttach?: any;
  previousVisible: boolean | undefined;
  isHidden: boolean;
}
