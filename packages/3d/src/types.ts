// JSX typings for @krate/3d scene elements and component props.

export type Vec3 = [number, number, number];
export type Vec2 = [number, number];
/** A CSS color string or a 0xRRGGBB number. */
export type ColorRepresentation = string | number;
/** URL of an image used as a texture. */
export type TextureSource = string;

export interface Pointer3DEvent {
  /** Entity id of the picked mesh. */
  entity: number;
  /** Pointer position in canvas pixels. */
  x: number;
  y: number;
  nativeEvent: PointerEvent | MouseEvent;
}
export type PointerHandler = (event: Pointer3DEvent) => void;

export interface EventProps {
  onClick?: PointerHandler;
  onPointerOver?: PointerHandler;
  onPointerOut?: PointerHandler;
  onPointerMove?: PointerHandler;
  onPointerDown?: PointerHandler;
  onPointerUp?: PointerHandler;
}

export interface TransformProps {
  position?: Vec3;
  /** Euler angles in radians, XYZ order. */
  rotation?: Vec3;
  scale?: Vec3 | number;
}

export interface NodeProps extends TransformProps {
  key?: string | number;
  name?: string;
  visible?: boolean;
  children?: unknown;
}

export interface MeshProps extends NodeProps, EventProps {
  castShadow?: boolean;
  receiveShadow?: boolean;
}
export type GroupProps = NodeProps;

export interface GeometryProps<A extends unknown[]> {
  args?: A;
}
export type BoxGeometryProps = GeometryProps<[width?: number, height?: number, depth?: number]>;
export type SphereGeometryProps = GeometryProps<
  [radius?: number, widthSegments?: number, heightSegments?: number]
>;
export type PlaneGeometryProps = GeometryProps<
  [width?: number, height?: number, widthSegments?: number, heightSegments?: number]
>;

export interface MaterialMaps {
  map?: TextureSource;
  normalMap?: TextureSource;
  aoMap?: TextureSource;
  roughnessMap?: TextureSource;
  metalnessMap?: TextureSource;
  emissiveMap?: TextureSource;
  normalScale?: number;
  aoMapIntensity?: number;
}

export interface MeshBasicMaterialProps {
  color?: ColorRepresentation;
  map?: TextureSource;
  opacity?: number;
  transparent?: boolean;
}

export interface MeshStandardMaterialProps extends MeshBasicMaterialProps, MaterialMaps {
  roughness?: number;
  metallic?: number;
  emissive?: ColorRepresentation;
  emissiveIntensity?: number;
}

export interface MeshPhysicalMaterialProps extends MeshStandardMaterialProps {
  clearcoat?: number;
  clearcoatRoughness?: number;
  sheenColor?: ColorRepresentation;
  sheenRoughness?: number;
  transmission?: number;
  ior?: number;
}

export interface ShaderMaterialProps {
  /** GLSL body of the Filament `material(inout MaterialInputs)` function. */
  fragmentShader?: string;
  /** Extra material parameters, name to type (`float`, `float3`, `float4`, `sampler2d`). */
  uniforms?: Record<string, number | Vec3 | [number, number, number, number] | TextureSource>;
  transparent?: boolean;
  opacity?: number;
}

export interface LightProps {
  color?: ColorRepresentation;
  /** Three.js-scale intensity; the compiler converts it to Filament units. */
  intensity?: number;
  position?: Vec3;
  castShadow?: boolean;
}
export type DirectionalLightProps = LightProps;
export interface PointLightProps extends LightProps {
  /** Falloff radius in world units. */
  distance?: number;
}
export interface SpotLightProps extends PointLightProps {
  /** Target the light points at. */
  target?: Vec3;
  /** Outer cone angle in radians. */
  angle?: number;
  /** Inner-to-outer cone softness, 0 to 1. */
  penumbra?: number;
}
export interface AmbientLightProps {
  color?: ColorRepresentation;
  intensity?: number;
}

export type ToneMapping = 'aces' | 'filmic' | 'agx' | 'linear';
export type Antialias = 'taa' | 'fxaa' | 'msaa' | 'none';

export interface BloomOptions {
  strength?: number;
  levels?: number;
  threshold?: boolean;
}
export interface SsaoOptions {
  radius?: number;
  intensity?: number;
  bias?: number;
}
export interface CameraProps {
  position?: Vec3;
  /** Point the camera looks at. */
  target?: Vec3;
  /** Vertical field of view in degrees. */
  fov?: number;
  near?: number;
  far?: number;
}

export interface CanvasProps {
  class?: string;
  style?: string | Record<string, string | number>;
  shadows?: boolean;
  bloom?: boolean | BloomOptions;
  ssao?: boolean | SsaoOptions;
  antialias?: Antialias;
  toneMapping?: ToneMapping;
  camera?: CameraProps;
  /** Exposure compensation in EV stops added to the default f/16, 1/125 s, ISO 100 camera. */
  exposure?: number;
  children?: unknown;
}

export interface OrbitControlsProps {
  target?: Vec3;
  enableZoom?: boolean;
  enablePan?: boolean;
  enableRotate?: boolean;
  autoRotate?: boolean;
  autoRotateSpeed?: number;
  minDistance?: number;
  maxDistance?: number;
}

export interface EnvironmentProps {
  /** Built-in preset name, or a URL to a KTX image-based light. */
  preset?: 'studio' | 'sunset' | 'dawn' | 'night' | 'warehouse' | 'forest' | 'city';
  files?: string;
  /** Show the environment as the scene background. */
  background?: boolean;
  intensity?: number;
}

export interface SceneElements {
  mesh: MeshProps;
  group: GroupProps;
  boxGeometry: BoxGeometryProps;
  sphereGeometry: SphereGeometryProps;
  planeGeometry: PlaneGeometryProps;
  meshStandardMaterial: MeshStandardMaterialProps;
  meshPhysicalMaterial: MeshPhysicalMaterialProps;
  meshBasicMaterial: MeshBasicMaterialProps;
  shaderMaterial: ShaderMaterialProps;
  directionalLight: DirectionalLightProps;
  pointLight: PointLightProps;
  spotLight: SpotLightProps;
  ambientLight: AmbientLightProps;
}

declare global {
  namespace JSX {
    interface IntrinsicElements extends SceneElements {}
  }
}
