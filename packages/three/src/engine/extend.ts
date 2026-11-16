// Constructor registration and lookup for intrinsic three elements.

/** A class that intrinsic elements can instantiate. */
export type ConstructorRepresentation<T = any> = new (...args: any[]) => T;

/** Registered constructors, keyed by PascalCase name. */
export interface Catalogue {
  [name: string]: ConstructorRepresentation;
}

const catalogue: Catalogue = {};
let nextId = 0;

/** Converts an element name to its catalogue key: `mesh` is `Mesh`. */
export const toPascalCase = (type: string): string => `${type[0].toUpperCase()}${type.slice(1)}`;

/** Reports whether `name` (PascalCase) is registered. */
export function hasConstructor(name: string): boolean {
  return name in catalogue;
}

/** Returns the constructor registered for `name` (PascalCase). */
export function resolveConstructor(name: string): ConstructorRepresentation | undefined {
  return catalogue[name];
}

/**
 * Registers classes as intrinsic elements: after `extend({ OrbitControls })`,
 * `<orbitControls />` creates one. Passing a single class registers it under a
 * generated name and returns that name, which works as a tag:
 * `const Controls = extend(OrbitControls); <Controls />`.
 */
export function extend<T extends ConstructorRepresentation>(objects: T): string;
export function extend<T extends Catalogue>(objects: T): void;
export function extend(objects: ConstructorRepresentation | Catalogue): string | void {
  if (typeof objects === 'function') {
    const name = `${nextId++}`;
    catalogue[name] = objects;
    return name;
  }
  Object.assign(catalogue, objects);
}
