// The types of react-konva's API (ReactKonvaCore.d.ts), written so that they
// type-check under Krate's, Preact's and React's JSX.

import type Konva from 'konva';

type KonvaEventObject<E> = Konva.KonvaEventObject<E>;

// Methods (not function properties) so handlers taking a narrower event
// type, as react-konva allows, are accepted.
export interface KonvaNodeEvents {
  onMouseOver?(evt: KonvaEventObject<MouseEvent>): void;
  onMouseMove?(evt: KonvaEventObject<MouseEvent>): void;
  onMouseOut?(evt: KonvaEventObject<MouseEvent>): void;
  onMouseEnter?(evt: KonvaEventObject<MouseEvent>): void;
  onMouseLeave?(evt: KonvaEventObject<MouseEvent>): void;
  onMouseDown?(evt: KonvaEventObject<MouseEvent>): void;
  onMouseUp?(evt: KonvaEventObject<MouseEvent>): void;
  onWheel?(evt: KonvaEventObject<WheelEvent>): void;
  onClick?(evt: KonvaEventObject<MouseEvent>): void;
  onDblClick?(evt: KonvaEventObject<MouseEvent>): void;
  onTouchStart?(evt: KonvaEventObject<TouchEvent>): void;
  onTouchMove?(evt: KonvaEventObject<TouchEvent>): void;
  onTouchEnd?(evt: KonvaEventObject<TouchEvent>): void;
  onTouchCancel?(evt: KonvaEventObject<TouchEvent>): void;
  onTap?(evt: KonvaEventObject<TouchEvent>): void;
  onDblTap?(evt: KonvaEventObject<TouchEvent>): void;
  onDragStart?(evt: KonvaEventObject<DragEvent>): void;
  onDragMove?(evt: KonvaEventObject<DragEvent>): void;
  onDragEnd?(evt: KonvaEventObject<DragEvent>): void;
  onTransform?(evt: KonvaEventObject<Event>): void;
  onTransformStart?(evt: KonvaEventObject<Event>): void;
  onTransformEnd?(evt: KonvaEventObject<Event>): void;
  onContextMenu?(evt: KonvaEventObject<PointerEvent>): void;
  onPointerDown?(evt: KonvaEventObject<PointerEvent>): void;
  onPointerMove?(evt: KonvaEventObject<PointerEvent>): void;
  onPointerUp?(evt: KonvaEventObject<PointerEvent>): void;
  onPointerCancel?(evt: KonvaEventObject<PointerEvent>): void;
  onPointerEnter?(evt: KonvaEventObject<PointerEvent>): void;
  onPointerLeave?(evt: KonvaEventObject<PointerEvent>): void;
  onPointerOver?(evt: KonvaEventObject<PointerEvent>): void;
  onPointerOut?(evt: KonvaEventObject<PointerEvent>): void;
  onPointerClick?(evt: KonvaEventObject<PointerEvent>): void;
  onPointerDblClick?(evt: KonvaEventObject<PointerEvent>): void;
  onGotPointerCapture?(evt: KonvaEventObject<PointerEvent>): void;
  onLostPointerCapture?(evt: KonvaEventObject<PointerEvent>): void;
}

/** A ref to a Konva node: an object ref, a callback ref, or (in Krate pages) a variable. */
export type KonvaRef<Node> = { current: Node | null } | ((node: Node | null) => unknown) | Node | null | undefined;

/** Attributes every element takes besides its Konva config. */
export interface KonvaElementAttributes<Node> {
  ref?: KonvaRef<Node>;
  key?: string | number | bigint | null;
  children?: unknown;
  /** Compares props with the node's attributes on update (see useStrictMode). */
  _useStrictMode?: boolean;
}

/**
 * An element of the stage. The value is react-konva's: the node type name
 * ('Rect'); the type is a component so JSX type-checks its props.
 */
export interface KonvaNodeComponent<Node extends Konva.Node, Props = Konva.NodeConfig> {
  (props: Props & KonvaNodeEvents & KonvaElementAttributes<Node>): any;
}

export interface StageProps extends Konva.NodeConfig, KonvaNodeEvents {
  className?: string;
  class?: string;
  id?: string;
  role?: string;
  style?: Record<string, string | number | undefined> | string;
  tabIndex?: number;
  title?: string;
  accessKey?: string;
  children?: unknown;
  /**
   * Wraps native Konva input in a batch (Konva 10.5+). Must call its
   * callback exactly once, synchronously.
   */
  eventBatchFunc?: (callback: () => void) => void;
}
