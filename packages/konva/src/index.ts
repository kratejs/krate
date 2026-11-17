// @krate/konva: a Konva renderer for Krate with the react-konva API.
//
// This module is Krate source: <Stage> compiles with the page. The elements
// are react-konva's: their values are the Konva class names, which the
// browser engine (`@krate/konva/engine`, bundled like an npm dependency so
// the app and the engine share one konva) turns into nodes.

import * as engine from '@krate/konva/engine';
import type Konva from 'konva';
import type { KonvaNodeComponent } from '@krate/konva/engine';

export { Stage } from './stage.js';
export type { StageProps } from './stage.js';
export type { KonvaNodeEvents, KonvaNodeComponent, KonvaRef } from '@krate/konva/engine';

export const Layer = 'Layer' as unknown as KonvaNodeComponent<Konva.Layer, Konva.LayerConfig>;
export const FastLayer = 'FastLayer' as unknown as KonvaNodeComponent<Konva.FastLayer, Konva.LayerConfig>;
export const Group = 'Group' as unknown as KonvaNodeComponent<Konva.Group, Konva.ContainerConfig>;
export const Label = 'Label' as unknown as KonvaNodeComponent<Konva.Label, Konva.LabelConfig>;
export const Rect = 'Rect' as unknown as KonvaNodeComponent<Konva.Rect, Konva.RectConfig>;
export const Circle = 'Circle' as unknown as KonvaNodeComponent<Konva.Circle, Konva.CircleConfig>;
export const Ellipse = 'Ellipse' as unknown as KonvaNodeComponent<Konva.Ellipse, Konva.EllipseConfig>;
export const Wedge = 'Wedge' as unknown as KonvaNodeComponent<Konva.Wedge, Konva.WedgeConfig>;
export const Transformer = 'Transformer' as unknown as KonvaNodeComponent<Konva.Transformer, Konva.TransformerConfig>;
export const Line = 'Line' as unknown as KonvaNodeComponent<Konva.Line, Konva.LineConfig>;
export const Sprite = 'Sprite' as unknown as KonvaNodeComponent<Konva.Sprite, Konva.SpriteConfig>;
export const Image = 'Image' as unknown as KonvaNodeComponent<Konva.Image, Konva.ImageConfig>;
export const Text = 'Text' as unknown as KonvaNodeComponent<Konva.Text, Konva.TextConfig>;
export const TextPath = 'TextPath' as unknown as KonvaNodeComponent<Konva.TextPath, Konva.TextPathConfig>;
export const Star = 'Star' as unknown as KonvaNodeComponent<Konva.Star, Konva.StarConfig>;
export const Ring = 'Ring' as unknown as KonvaNodeComponent<Konva.Ring, Konva.RingConfig>;
export const Arc = 'Arc' as unknown as KonvaNodeComponent<Konva.Arc, Konva.ArcConfig>;
export const Tag = 'Tag' as unknown as KonvaNodeComponent<Konva.Tag, Konva.TagConfig>;
export const Path = 'Path' as unknown as KonvaNodeComponent<Konva.Path, Konva.PathConfig>;
export const RegularPolygon = 'RegularPolygon' as unknown as KonvaNodeComponent<Konva.RegularPolygon, Konva.RegularPolygonConfig>;
export const Arrow = 'Arrow' as unknown as KonvaNodeComponent<Konva.Arrow, Konva.ArrowConfig>;
export const Shape = 'Shape' as unknown as KonvaNodeComponent<Konva.Shape, Konva.ShapeConfig>;

/**
 * Strict mode: on every update the props are compared with the node's
 * attributes (not only with the previous props), so a node moved by drag or
 * transform is reset to its props.
 */
export const useStrictMode: (useStrictMode: boolean) => void = engine.toggleStrictMode;

export const version: string = engine.version;

function Bridge(props: { children?: unknown }): any {
  return props.children;
}

/**
 * react-konva (its-fine) API: returns a component that passes the context
 * around the Stage to its children. Krate already does that, so it renders
 * its children as they are.
 */
export function useContextBridge(): (props: { children?: unknown }) => any {
  return Bridge;
}
