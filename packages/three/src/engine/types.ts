// JSX element types for three classes. Ported from @react-three/fiber (MIT,
// Copyright (c) 2019-2025 Poimandres), see NOTICE.

import type * as THREE from 'three';
import type { EventHandlers } from './events.js';
import type { ConstructorRepresentation } from './extend.js';
import type { Args, InstanceProps } from './instance.js';
import type { Mutable, Overwrite } from './utils.js';

type MutableOrReadonlyParameters<T extends (...args: any) => any> = Parameters<T> | Readonly<Parameters<T>>;

export interface MathRepresentation {
  set(...args: number[]): any;
}
export interface VectorRepresentation extends MathRepresentation {
  setScalar(value: number): any;
}

export type MathTypes = MathRepresentation | THREE.Euler | THREE.Color;

export type MathType<T extends MathTypes> = T extends THREE.Color
  ? Args<typeof THREE.Color> | THREE.ColorRepresentation
  : T extends VectorRepresentation | THREE.Layers | THREE.Euler
    ? T | MutableOrReadonlyParameters<T['set']> | number
    : T | MutableOrReadonlyParameters<T['set']>;

export type MathProps<P> = {
  [K in keyof P as P[K] extends MathTypes ? K : never]: P[K] extends MathTypes ? MathType<P[K]> : never;
};

export type Vector2 = MathType<THREE.Vector2>;
export type Vector3 = MathType<THREE.Vector3>;
export type Vector4 = MathType<THREE.Vector4>;
export type Color = MathType<THREE.Color>;
export type Layers = MathType<THREE.Layers>;
export type Quaternion = MathType<THREE.Quaternion>;
export type Euler = MathType<THREE.Euler>;
export type Matrix3 = MathType<THREE.Matrix3>;
export type Matrix4 = MathType<THREE.Matrix4>;

export interface RaycastableRepresentation {
  raycast(raycaster: THREE.Raycaster, intersects: THREE.Intersection[]): void;
}

export type EventProps<P> = P extends RaycastableRepresentation ? Partial<EventHandlers> : {};

/** The JSX attributes every scene element accepts (children, ref, key). */
export interface ReactProps<P> {
  children?: unknown;
  ref?: { current: P | null } | ((instance: P) => void) | P | null;
  key?: string | number;
}

export type ElementProps<T extends ConstructorRepresentation, P = InstanceType<T>> = Partial<
  Overwrite<P, MathProps<P> & ReactProps<P> & EventProps<P>>
>;

export type ThreeElement<T extends ConstructorRepresentation> = Mutable<
  Overwrite<ElementProps<T>, Omit<InstanceProps<InstanceType<T>, T>, 'object'>>
>;

export type ThreeToJSXElements<T extends Record<string, any>> = {
  [K in keyof T & string as T[K] extends ConstructorRepresentation ? Uncapitalize<K> : never]: ThreeElement<T[K]>;
};

type ThreeExports = typeof THREE;
type ThreeElementsImpl = ThreeToJSXElements<ThreeExports>;

/**
 * The intrinsic scene elements: every three class, camelCased (`<mesh>`,
 * `<boxGeometry>`), plus `<primitive object>`. The names that clash with
 * HTML or SVG elements take a `three` prefix.
 */
export interface ThreeElements extends Omit<ThreeElementsImpl, 'audio' | 'source' | 'line' | 'path'> {
  primitive: Omit<ThreeElement<any>, 'args'> & { object: object };
  threeAudio: ThreeElementsImpl['audio'];
  threeSource: ThreeElementsImpl['source'];
  threeLine: ThreeElementsImpl['line'];
  threePath: ThreeElementsImpl['path'];
}
