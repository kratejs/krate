import { onMount } from '@krate/runtime';
import { createRoot } from '@krate/fakethree/engine';

@renderer
export function Canvas(props: { children?: unknown; frameloop?: string }) {
  let canvas: HTMLCanvasElement | undefined;
  onMount(() => {
    createRoot(canvas!).render(props.children as () => unknown[]);
  });
  return <div style={{ position: 'relative' }}><canvas ref={canvas} /></div>;
}
