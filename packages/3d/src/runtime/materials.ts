// Uber-material loading, parameter defaults and live parameter updates.

import { colorToLinear } from './color.js';
import type { FEngine, FMaterial, FMaterialInstance, FTexture, FilamentModule } from './filament.js';

export type MaterialKind = 'standard' | 'physical' | 'unlit';
/** Kind of a handle: the uber-materials, or a build-compiled shaderMaterial. */
export type HandleKind = MaterialKind | 'custom';
export type ParamValue = number | boolean | string | readonly number[];
export type MaterialParams = Record<string, ParamValue>;

const MAPS = ['baseColor', 'normal', 'ao', 'roughness', 'metallic', 'emissive'] as const;
const STANDARD_FLOATS: Record<string, number> = {
  roughness: 1,
  metallic: 0,
  emissiveIntensity: 1,
  opacity: 1,
  normalScale: 1,
  aoMapIntensity: 1,
};
const PHYSICAL_FLOATS: Record<string, number> = {
  clearcoat: 0,
  clearcoatRoughness: 0,
  sheenRoughness: 1,
  transmission: 0,
  ior: 1.5,
};

/** Default value of every non-sampler parameter, per material kind (DESIGN.md parameter table). */
export function materialDefaults(kind: MaterialKind): Record<string, number | number[]> {
  const out: Record<string, number | number[]> = { baseColor: [1, 1, 1, 1], opacity: 1 };
  if (kind !== 'unlit') {
    Object.assign(out, STANDARD_FLOATS, { emissive: [0, 0, 0] });
    for (const m of MAPS) out[`use${m[0].toUpperCase()}${m.slice(1)}Map`] = 0;
    if (kind === 'physical') Object.assign(out, PHYSICAL_FLOATS, { sheenColor: [0, 0, 0] });
  } else {
    out.useBaseColorMap = 0;
  }
  return out;
}

const COLOR_PARAMS = new Set(['baseColor', 'emissive', 'sheenColor']);
const FLOAT3 = new Set(['emissive', 'sheenColor']);

function mapNames(kind: MaterialKind): string[] {
  return (kind === 'unlit' ? ['baseColor'] : [...MAPS]).map((m) => `${m}Map`);
}

/** Asset URL of a compiled material, relative to this module (bundlers rewrite the literal). */
const FILES: Record<string, () => URL> = {
  standard: () => new URL('../../materials/standard.filamat', import.meta.url),
  standard_transparent: () => new URL('../../materials/standard_transparent.filamat', import.meta.url),
  physical: () => new URL('../../materials/physical.filamat', import.meta.url),
  physical_transparent: () => new URL('../../materials/physical_transparent.filamat', import.meta.url),
  unlit: () => new URL('../../materials/unlit.filamat', import.meta.url),
  unlit_transparent: () => new URL('../../materials/unlit_transparent.filamat', import.meta.url),
};

export function materialUrl(kind: MaterialKind, transparent: boolean, base?: string): string {
  const name = transparent ? `${kind}_transparent` : kind;
  if (base !== undefined) return `${base.replace(/\/?$/, '/')}${name}.filamat`;
  return FILES[name]().href;
}

/** True when the transparent (blended) variant is needed. */
export function isTransparent(params: MaterialParams): boolean {
  const opacity = params.opacity;
  return params.transparent === true || (typeof opacity === 'number' && opacity < 1);
}

const KNOWN = new Map<MaterialKind, Set<string>>();

/** Whether the compiled material of this kind declares the parameter (unknown names must not reach Filament). */
export function isKnownParam(kind: MaterialKind, name: string): boolean {
  let set = KNOWN.get(kind);
  if (!set) {
    set = new Set([...Object.keys(materialDefaults(kind)), ...mapNames(kind)]);
    KNOWN.set(kind, set);
  }
  return set.has(name);
}

function isTextureUrl(name: string, v: ParamValue): v is string {
  return typeof v === 'string' && name.endsWith('Map');
}

/** Spec of a build-compiled shaderMaterial: its .filamat URL and initial uniform values. */
export interface CustomMaterialSpec {
  url: string;
  params?: Record<string, number | readonly number[]>;
  transparent?: boolean;
}

export interface MaterialHandle {
  kind: HandleKind;
  /** Compiled material file of a custom material. */
  url?: string;
  params: MaterialParams;
  transparent: boolean;
  instance?: FMaterialInstance;
  /** Callbacks run once the instance exists. */
  waiters: Array<() => void>;
}

export class MaterialManager {
  private files = new Map<string, Promise<FMaterial>>();
  private materials: FMaterial[] = [];
  private instances: FMaterialInstance[] = [];
  private textures: FTexture[] = [];
  private placeholders = new Map<string, FTexture>();
  private sampler: unknown;
  private disposed = false;

  constructor(
    private F: FilamentModule,
    private engine: FEngine,
    private base?: string,
    private fetchBytes: (url: string) => Promise<Uint8Array> = defaultFetch
  ) {}

  private load(kind: HandleKind, transparent: boolean, customUrl?: string): Promise<FMaterial> {
    const url = customUrl ?? materialUrl(kind as MaterialKind, transparent, this.base);
    let p = this.files.get(url);
    if (!p) {
      p = this.fetchBytes(url).then((bytes) => {
        const m = this.engine.createMaterial(bytes);
        this.materials.push(m);
        return m;
      });
      this.files.set(url, p);
    }
    return p;
  }

  /** Creates a handle without touching the engine; see start(). */
  static handle(kind: MaterialKind, params: MaterialParams): MaterialHandle {
    return { kind, params: { ...params }, transparent: isTransparent(params), waiters: [] };
  }

  /** Handle for a compiled shaderMaterial; its params are plain uniform values. */
  static customHandle(spec: CustomMaterialSpec): MaterialHandle {
    return { kind: 'custom', url: spec.url, params: { ...spec.params }, transparent: !!spec.transparent, waiters: [] };
  }

  /** Starts loading the material file and creates the instance when it is ready. */
  start(handle: MaterialHandle): MaterialHandle {
    void this.load(handle.kind, handle.transparent, handle.url).then(
      (mat) => {
        if (this.disposed) return;
        const inst = mat.createInstance();
        this.instances.push(inst);
        handle.instance = inst;
        this.applyAll(handle);
        for (const w of handle.waiters.splice(0)) w();
      },
      (err) => console.error(err)
    );
    return handle;
  }

  private applyAll(h: MaterialHandle): void {
    if (h.kind === 'custom') {
      for (const [name, value] of Object.entries(h.params)) this.apply(h, name, value);
      return;
    }
    const defaults = materialDefaults(h.kind);
    const merged: MaterialParams = { ...defaults, ...h.params };
    for (const name of mapNames(h.kind)) this.setPlaceholder(h, name);
    for (const [name, value] of Object.entries(merged)) this.apply(h, name, value);
  }

  private setPlaceholder(h: MaterialHandle, name: string): void {
    // A flat normal for normalMap, white for everything else.
    const key = name === 'normalMap' ? 'flat' : 'white';
    let tex = this.placeholders.get(key);
    if (!tex) {
      const F = this.F;
      const px = key === 'flat' ? new Uint8Array([128, 128, 255, 255]) : new Uint8Array([255, 255, 255, 255]);
      tex = F.Texture.Builder()
        .width(1)
        .height(1)
        .levels(1)
        .sampler(F.Texture$Sampler.SAMPLER_2D)
        .format(F.Texture$InternalFormat.RGBA8)
        .build(this.engine);
      (tex as unknown as { setImage(e: FEngine, l: number, p: unknown): void }).setImage(
        this.engine,
        0,
        F.PixelBuffer(px, F.PixelDataFormat.RGBA, F.PixelDataType.UBYTE)
      );
      this.textures.push(tex);
      this.placeholders.set(key, tex);
    }
    h.instance!.setTextureParameter(name, tex, this.getSampler());
  }

  private getSampler(): unknown {
    const F = this.F;
    return (this.sampler ??= new F.TextureSampler(F.MinFilter.LINEAR_MIPMAP_LINEAR, F.MagFilter.LINEAR, F.WrapMode.REPEAT));
  }

  private apply(h: MaterialHandle, name: string, value: ParamValue): void {
    const inst = h.instance!;
    if (h.kind === 'custom') {
      // Uniforms are passed through as declared by the compiled material.
      if (typeof value === 'number') inst.setFloatParameter(name, value);
      else if (typeof value !== 'string' && typeof value !== 'boolean') {
        if (value.length === 3) inst.setFloat3Parameter(name, value);
        else if (value.length === 4) inst.setFloat4Parameter(name, value);
      }
      return;
    }
    if (!isKnownParam(h.kind, name)) return;
    if (isTextureUrl(name, value)) {
      void this.applyTexture(h, name, value);
      return;
    }
    if (COLOR_PARAMS.has(name) && (typeof value === 'string' || (typeof value === 'number' && name !== 'opacity'))) {
      const [r, g, b] = colorToLinear(value);
      if (FLOAT3.has(name)) inst.setFloat3Parameter(name, [r, g, b]);
      else inst.setFloat4Parameter(name, [r, g, b, 1]);
      return;
    }
    if (typeof value === 'number') inst.setFloatParameter(name, value);
    else if (typeof value === 'boolean') inst.setBoolParameter(name, value);
    else if (value.length === 3) inst.setFloat3Parameter(name, value);
    else if (value.length === 4) inst.setFloat4Parameter(name, value);
  }

  private async applyTexture(h: MaterialHandle, name: string, url: string): Promise<void> {
    const bytes = await this.fetchBytes(url);
    if (this.disposed) return;
    const srgb = name === 'baseColorMap' || name === 'emissiveMap';
    const e = this.engine;
    const lower = url.split('?')[0].toLowerCase();
    const tex = lower.endsWith('.ktx2')
      ? e.createTextureFromKtx2(bytes, { srgb })
      : lower.endsWith('.jpg') || lower.endsWith('.jpeg')
        ? e.createTextureFromJpeg(bytes, { srgb })
        : e.createTextureFromPng(bytes, { srgb });
    this.textures.push(tex);
    h.instance!.setTextureParameter(name, tex, this.getSampler());
    h.instance!.setFloatParameter(`use${name[0].toUpperCase()}${name.slice(1)}`, 1);
  }

  /** Reactive update: remembers the value and writes it when the instance exists. */
  set(h: MaterialHandle, name: string, value: ParamValue): void {
    h.params[name] = value;
    if (h.instance) this.apply(h, name, value);
  }

  dispose(): void {
    this.disposed = true;
    for (const i of this.instances) this.engine.destroyMaterialInstance(i);
    for (const t of this.textures) this.engine.destroyTexture(t);
    for (const m of this.materials) this.engine.destroyMaterial(m);
    this.instances = [];
    this.textures = [];
    this.materials = [];
  }
}

export async function defaultFetch(url: string): Promise<Uint8Array> {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`@krate/3d: failed to load ${url} (${res.status})`);
  return new Uint8Array(await res.arrayBuffer());
}
