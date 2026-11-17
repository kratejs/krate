// What <Stage> does once its container div is in the document: create the
// Konva.Stage, apply its props (live), pass it to the ref and mount the
// children. Follows react-konva's Stage (MIT, Copyright (c) 2017-present
// Lavrenov Anton), see NOTICE.

import type KonvaType from 'konva';
import { Konva } from './konva.js';
import { applyNodeProps, EVENTS_NAMESPACE } from './props.js';
import { mountTree, type MountedTree } from './reconciler.js';
import { rt, setRef } from './runtime.js';

/** Props of the Stage that belong to its container div, not to the Konva.Stage. */
export const STAGE_DOM_PROPS = ['class', 'className', 'id', 'role', 'tabIndex', 'title', 'accessKey'];

/** Reads every prop but children and ref (reading subscribes to reactive props). */
function stageProps(props: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const key of Object.keys(props)) {
    if (key === 'children' || key === 'ref' || STAGE_DOM_PROPS.includes(key)) continue;
    out[key] = props[key];
  }
  return out;
}

/**
 * Mounts a stage into `container`. `getProps` returns the current Stage props
 * (reading them subscribes to the reactive ones, so a changed prop updates
 * the stage in place). Returns the teardown.
 */
export function mountStage(container: HTMLDivElement, getProps: () => Record<string, unknown>): () => void {
  const runtime = rt();
  const first = runtime.untrack(getProps);
  const stage = new Konva.Stage({
    width: first.width as number,
    height: first.height as number,
    container,
  });
  let prev: Record<string, unknown> = {};
  let tree: MountedTree | null = null;
  let releaseRef: () => void = () => {};
  let ref: unknown;
  let bound = false;

  const dispose = runtime.createRoot((d) => {
    runtime.createEffect(() => {
      const props = getProps();
      const next = stageProps(props);
      const nextRef = props.ref;
      runtime.untrack(() => {
        applyNodeProps(stage, next, prev);
        prev = next;
        if (nextRef !== ref || !bound) {
          bound = true;
          releaseRef();
          ref = nextRef;
          releaseRef = setRef(ref, stage);
        }
      });
    });
    return d;
  });
  // Children mount once: their reactive parts update themselves.
  tree = runtime.untrack(() => mountTree(first.children, stage as unknown as KonvaType.Container));

  return () => {
    dispose();
    tree?.unmount();
    tree = null;
    releaseRef();
    stage.destroy();
    stage.off(EVENTS_NAMESPACE);
  };
}
