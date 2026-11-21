import { onMount } from '@krate/runtime';
import { canvasStyle } from './canvas_style.js';
import type { CanvasProps } from '../types.js';

export interface CompiledCanvasProps {
  /** Compiled scene setup: creates the engine on the canvas and emits every engine call. */
  setup: (canvas: HTMLCanvasElement) => void;
  class?: string;
  style?: CanvasProps['style'];
  id?: string;
  role?: string;
  'aria-label'?: string;
  'aria-hidden'?: boolean | 'true' | 'false';
}

/**
 * The element the compiler replaces a `<Canvas>` with. It renders the DOM
 * canvas and runs the compiled setup once it is mounted; the scene disposes
 * itself with the component through the cleanup `__k3d.scene` registers.
 */
export function CompiledCanvas(props: CompiledCanvasProps) {
  let canvas: HTMLCanvasElement | undefined;
  onMount(() => {
    props.setup(canvas!);
  });
  return (
    <canvas
      ref={canvas}
      class={props.class}
      style={canvasStyle(props.style)}
      id={props.id}
      role={props.role}
      aria-label={props['aria-label']}
      aria-hidden={props['aria-hidden']}
    />
  );
}
