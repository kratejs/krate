import { onCleanup, onMount } from '@krate/runtime';
import { mountCanvas, type CanvasProps as EngineCanvasProps } from '@krate/three/engine';

/** Props of <Canvas>: renderer options plus the attributes of its wrapping div. */
export interface CanvasProps extends EngineCanvasProps {
  id?: string;
  class?: string;
  className?: string;
  style?: Record<string, string | number>;
  tabIndex?: number;
  role?: string;
  title?: string;
  'aria-label'?: string;
}

/**
 * A DOM canvas that renders its three.js children. The children are a scene
 * description (`<mesh>`, `<ambientLight>`, components using useFrame, ...):
 * they never render to HTML and are mounted in the browser once the canvas
 * has a size.
 */
// @ts-ignore TS1206: Krate removes component decorators at compile time.
@renderer
export function Canvas(props: CanvasProps) {
  let div: HTMLDivElement | undefined;
  let container: HTMLDivElement | undefined;
  let canvas: HTMLCanvasElement | undefined;
  onMount(() => {
    const teardown = mountCanvas({ div: div!, container: container!, canvas: canvas! }, () => props);
    onCleanup(teardown);
  });
  return (
    <div
      ref={div}
      id={props.id}
      class={props.class ?? props.className}
      tabIndex={props.tabIndex}
      role={props.role}
      title={props.title}
      aria-label={props['aria-label']}
      style={{
        position: 'relative',
        width: '100%',
        height: '100%',
        overflow: 'hidden',
        'pointer-events': props.eventSource ? 'none' : 'auto',
        ...props.style,
      }}
    >
      <div ref={container} style={{ width: '100%', height: '100%' }}>
        <canvas ref={canvas} style={{ display: 'block' }}>
          {props.fallback}
        </canvas>
      </div>
    </div>
  );
}
