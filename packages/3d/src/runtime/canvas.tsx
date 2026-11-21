import { onCleanup, onMount } from '@krate/runtime';
import { canvasStyle, mountCanvas, type CanvasMountProps } from './mount.js';

/**
 * A DOM canvas that renders its Filament scene. Scenes the compiler can
 * analyse never call this: they compile to direct engine calls. This is the
 * runtime fallback, which mounts the scene description in the browser.
 */
// @ts-ignore TS1206: Krate removes component decorators at compile time.
@renderer
export function Canvas(props: CanvasMountProps) {
  let canvas: HTMLCanvasElement | undefined;
  onMount(() => {
    const teardown = mountCanvas(canvas!, props);
    onCleanup(teardown);
  });
  return <canvas ref={canvas} class={props.class} style={canvasStyle(props.style)} />;
}
