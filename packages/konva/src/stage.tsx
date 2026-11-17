import { onCleanup, onMount } from '@krate/runtime';
import { mountStage, type KonvaRef, type StageProps as EngineStageProps } from '@krate/konva/engine';
import type Konva from 'konva';

/** Props of <Stage>: the Konva.Stage config and events plus the attributes of its container div. */
export interface StageProps extends EngineStageProps {
  ref?: KonvaRef<Konva.Stage>;
}

/**
 * A DOM container holding a Konva.Stage that renders its children (Layer,
 * Rect, components drawing shapes, ...). The children never render to HTML:
 * they are mounted in the browser once the container is in the document.
 */
// @ts-ignore TS1206: Krate removes component decorators at compile time.
@renderer
export function Stage(props: StageProps) {
  let div: HTMLDivElement | undefined;
  onMount(() => {
    const teardown = mountStage(div!, () => props as unknown as Record<string, unknown>);
    onCleanup(teardown);
  });
  return (
    <div
      ref={div}
      id={props.id}
      class={props.class ?? props.className}
      role={props.role}
      tabIndex={props.tabIndex}
      title={props.title}
      accessKey={props.accessKey}
      style={props.style as Record<string, string | number> | string | undefined}
    />
  );
}
