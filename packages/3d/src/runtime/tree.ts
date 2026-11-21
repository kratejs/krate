// Fallback path: mounts the element descriptors a renderer-tier <Canvas>
// receives (`{ __k3: 1, type, props, children }`, the same shape @krate/three
// consumes) onto a scene through the same API compiled code uses. There is no
// compile-time flattening: every mesh gets a dynamic slot, and groups are
// folded into a static parent matrix when mounted.

import type { PointerHandler } from '../types.js';
import { lightIntensity } from './calibration.js';
import { colorToLinear } from './color.js';
import type { GeometryKind } from './geometry.js';
import type { LightKind, LightProps } from './lights.js';
import type { MaterialKind, MaterialParams, ParamValue } from './materials.js';
import type { PointerEventName } from './picking.js';
import { rt } from './rt.js';
import type { Scene3D } from './scene.js';
import { composeTRS, eulerToQuat, mulMat4 } from './transforms.js';

type Props = Record<string, unknown>;

interface Element {
  __k3?: number;
  __kv?: number;
  __ka?: number;
  type?: unknown;
  tag?: string;
  props?: Props | null;
  children?: unknown;
  items?: () => unknown;
  render?: (item: unknown, index: unknown) => unknown;
}

const IDENTITY = [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1];
const GEOMETRIES: Record<string, GeometryKind> = { boxGeometry: 'box', sphereGeometry: 'sphere', planeGeometry: 'plane' };
const MATERIALS: Record<string, MaterialKind> = {
  meshStandardMaterial: 'standard',
  meshPhysicalMaterial: 'physical',
  meshBasicMaterial: 'unlit',
};
const LIGHTS: Record<string, LightKind> = {
  directionalLight: 'directional',
  pointLight: 'point',
  spotLight: 'spot',
  ambientLight: 'ambient',
};
const EVENTS: Record<string, PointerEventName> = {
  onClick: 'click',
  onPointerOver: 'pointerover',
  onPointerOut: 'pointerout',
  onPointerMove: 'pointermove',
  onPointerDown: 'pointerdown',
  onPointerUp: 'pointerup',
};
// Three.js-style material prop to uber-material parameter.
const MATERIAL_PROPS: Record<string, string> = {
  roughness: 'roughness',
  metallic: 'metallic',
  metalness: 'metallic',
  emissiveIntensity: 'emissiveIntensity',
  opacity: 'opacity',
  transparent: 'transparent',
  map: 'baseColorMap',
  normalMap: 'normalMap',
  aoMap: 'aoMap',
  roughnessMap: 'roughnessMap',
  metalnessMap: 'metallicMap',
  emissiveMap: 'emissiveMap',
  normalScale: 'normalScale',
  aoMapIntensity: 'aoMapIntensity',
  clearcoat: 'clearcoat',
  clearcoatRoughness: 'clearcoatRoughness',
  sheenRoughness: 'sheenRoughness',
  transmission: 'transmission',
  ior: 'ior',
};

function isThunk(key: string, v: unknown): v is () => unknown {
  return typeof v === 'function' && (v as Function).length === 0 && !(key in EVENTS) && key !== 'ref';
}

/** A Three.js-style material prop as a uber-material parameter value. */
export function materialParam(key: string, value: unknown): [string, ParamValue] | undefined {
  if (key === 'color') {
    const [r, g, b] = colorToLinear(value as string | number);
    return ['baseColor', [r, g, b, 1]];
  }
  if (key === 'emissive' || key === 'sheenColor') return [key, colorToLinear(value as string | number)];
  const name = MATERIAL_PROPS[key];
  return name ? [name, value as ParamValue] : undefined;
}

export function materialParams(props: Props, read: (key: string) => unknown = (k) => props[k]): MaterialParams {
  const out: MaterialParams = {};
  for (const key of Object.keys(props)) {
    const p = materialParam(key, read(key));
    if (p) out[p[0]] = p[1];
  }
  return out;
}

function trsMatrix(pos?: readonly number[], rot?: readonly number[], scale?: readonly number[] | number): number[] {
  const data = new Float32Array(10);
  data[6] = 1;
  data[7] = data[8] = data[9] = 1;
  if (pos) data.set(pos.slice(0, 3), 0);
  if (rot) eulerToQuat(data, 3, rot[0], rot[1], rot[2]);
  if (typeof scale === 'number') data.fill(scale, 7, 10);
  else if (scale) data.set(scale.slice(0, 3), 7);
  const m = new Float32Array(16);
  composeTRS(m, data, 0);
  return Array.from(m);
}

function multiply(a: number[], b: number[]): number[] {
  const out = new Float32Array(16);
  mulMat4(out, Float32Array.from(a), 0, Float32Array.from(b));
  return Array.from(out);
}

function transformPoint(m: number[], p: readonly number[]): number[] {
  const [x, y, z] = p;
  return [m[0] * x + m[4] * y + m[8] * z + m[12], m[1] * x + m[5] * y + m[9] * z + m[13], m[2] * x + m[6] * y + m[10] * z + m[14]];
}

function normalize(v: number[]): number[] {
  const l = Math.hypot(v[0], v[1], v[2]) || 1;
  return [v[0] / l, v[1] / l, v[2] / l];
}

interface Ctx {
  scene: Scene3D;
  parent: number[];
  disposers: Array<() => void>;
}

/** Mounts scene children; returns a function that stops their reactive effects. */
export function mountSceneTree(scene: Scene3D, children: unknown): () => void {
  const ctx: Ctx = { scene, parent: IDENTITY, disposers: [] };
  mountValue(children, ctx);
  return () => {
    for (const d of ctx.disposers.splice(0)) d();
  };
}

function mountValue(value: unknown, ctx: Ctx): void {
  if (value == null || typeof value === 'boolean' || typeof value === 'string' || typeof value === 'number') return;
  if (Array.isArray(value)) {
    for (const v of value) mountValue(v, ctx);
    return;
  }
  if (typeof value === 'function') {
    // Regions are evaluated once; the fallback does not diff scene children.
    mountValue(rt().untrack(value as () => unknown), ctx);
    return;
  }
  const el = value as Element;
  if (el.__ka === 1 && el.items && el.render) {
    const items = rt().untrack(el.items);
    if (Array.isArray(items)) items.forEach((item, i) => mountValue(el.render!(item, i), ctx));
    return;
  }
  if (el.__k3 === 2 && typeof el.type === 'function') {
    mountValue(rt().untrack(() => (el.type as (p: Props) => unknown)(el.props ?? {})), ctx);
    return;
  }
  if (el.__k3 === 1 && typeof el.type === 'string') return mountElement(el.type, el.props ?? {}, el.children, ctx);
  if (el.__kv === 1 && el.tag) return mountElement(el.tag, el.props ?? {}, el.children, ctx);
  console.warn('@krate/3d: unsupported scene child ignored', value);
}

function read(props: Props, key: string): unknown {
  const v = props[key];
  return isThunk(key, v) ? rt().untrack(v) : v;
}

/** Calls apply now and again whenever the (possibly reactive) prop changes. */
function track(ctx: Ctx, props: Props, key: string, apply: (v: unknown) => void): void {
  const v = props[key];
  if (isThunk(key, v)) ctx.disposers.push(rt().createEffect(() => apply(v())));
}

function childList(children: unknown): unknown[] {
  const out: unknown[] = [];
  const walk = (c: unknown): void => {
    if (Array.isArray(c)) c.forEach(walk);
    else if (c != null && typeof c === 'object') out.push(c);
  };
  walk(children);
  return out;
}

function mountElement(type: string, props: Props, children: unknown, ctx: Ctx): void {
  if (type === 'group') return mountGroup(props, children, ctx);
  if (type === 'mesh') return mountMesh(props, children, ctx);
  const kind = LIGHTS[type];
  if (kind) return mountLight(kind, props, ctx);
  console.warn(`@krate/3d: unknown scene element <${type}> ignored`);
}

function mountGroup(props: Props, children: unknown, ctx: Ctx): void {
  const m = trsMatrix(read(props, 'position') as number[], read(props, 'rotation') as number[], read(props, 'scale') as number[]);
  mountValue(children, { ...ctx, parent: multiply(ctx.parent, m) });
}

function elementOf(list: unknown[], names: Record<string, unknown>): { type: string; props: Props } | undefined {
  for (const c of list) {
    const e = c as Element;
    const t = e.type ?? e.tag;
    if (typeof t === 'string' && t in names) return { type: t, props: e.props ?? {} };
  }
  return undefined;
}

function mountMesh(props: Props, children: unknown, ctx: Ctx): void {
  const { scene } = ctx;
  const list = childList(children);
  const geo = elementOf(list, GEOMETRIES);
  const mat = elementOf(list, MATERIALS);
  if (!geo || !mat) {
    console.warn('@krate/3d: <mesh> needs a geometry and a material child');
    return;
  }
  const g = scene.geometry(GEOMETRIES[geo.type], (read(geo.props, 'args') as number[]) ?? []);
  const m = scene.material(MATERIALS[mat.type], materialParams(mat.props, (k) => read(mat.props, k)));
  for (const key of Object.keys(mat.props)) {
    track(ctx, mat.props, key, (v) => {
      const p = materialParam(key, v);
      if (p) scene.setMaterialParam(m, p[0], p[1]);
    });
  }
  const slot = scene.allocSlot();
  const pos = (read(props, 'position') as number[]) ?? [0, 0, 0];
  const rot = (read(props, 'rotation') as number[]) ?? [0, 0, 0];
  const sc = read(props, 'scale');
  const scale = typeof sc === 'number' ? [sc, sc, sc] : ((sc as number[]) ?? [1, 1, 1]);
  const q = new Float32Array(4);
  eulerToQuat(q, 0, rot[0], rot[1], rot[2]);
  const mesh = scene.dynamicMesh(slot, g, m, ctx.parent, { pos, quat: Array.from(q), scale }, {
    castShadow: !!read(props, 'castShadow'),
    receiveShadow: !!read(props, 'receiveShadow'),
  });
  track(ctx, props, 'position', (v) => scene.pos(slot, ...(v as [number, number, number])));
  track(ctx, props, 'rotation', (v) => scene.rot(slot, ...(v as [number, number, number])));
  track(ctx, props, 'scale', (v) => {
    const s = typeof v === 'number' ? [v, v, v] : (v as number[]);
    scene.scale(slot, s[0], s[1], s[2]);
  });
  for (const key of Object.keys(EVENTS)) {
    const h = props[key];
    if (typeof h === 'function') scene.on(mesh, EVENTS[key], h as PointerHandler);
  }
  // Nested meshes and lights are placed relative to this mesh's initial transform.
  const nested = list.filter((c) => {
    const t = (c as Element).type ?? (c as Element).tag;
    return typeof t !== 'string' || !(t in GEOMETRIES || t in MATERIALS);
  });
  if (nested.length) mountValue(nested, { ...ctx, parent: multiply(ctx.parent, trsMatrix(pos, rot, scale)) });
}

function mountLight(kind: LightKind, props: Props, ctx: Ctx): void {
  const { scene } = ctx;
  const position = transformPoint(ctx.parent, (read(props, 'position') as number[]) ?? [0, 0, 0]);
  const color = read(props, 'color') as string | number | undefined;
  const intensity = (read(props, 'intensity') as number | undefined) ?? 1;
  const lp: LightProps = { intensity: lightIntensity(kind, intensity), color, castShadows: !!read(props, 'castShadow') };
  if (kind === 'directional') {
    lp.position = position;
    lp.direction = normalize(position.map((c) => -c));
  } else if (kind !== 'ambient') {
    lp.position = position;
    const distance = read(props, 'distance') as number | undefined;
    if (distance) lp.falloff = distance;
    if (kind === 'spot') {
      const target = transformPoint(ctx.parent, (read(props, 'target') as number[]) ?? [0, 0, 0]);
      lp.direction = normalize(target.map((c, i) => c - position[i]));
      const angle = (read(props, 'angle') as number | undefined) ?? Math.PI / 3;
      const penumbra = (read(props, 'penumbra') as number | undefined) ?? 0;
      lp.outer = angle;
      lp.inner = angle * (1 - penumbra);
    }
  }
  const h = scene.light(kind, lp);
  track(ctx, props, 'intensity', (v) => scene.setLight(h, 'intensity', lightIntensity(kind, v as number)));
}
