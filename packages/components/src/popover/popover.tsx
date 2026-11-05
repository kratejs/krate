import './popover.css';
import { createScope, createScopeFlags, createCSSGroup, type CSSScope } from '@krate/runtime';
import { OverlayBridge } from '../overlay/overlay-bridge';

const PopoverCtx = createScope<string | null>();

export interface PopoverProps {
  label?: string;
  /** Value of the content shown initially (a literal, like Tabs `defaultValue`). */
  defaultValue?: string;
  /** Open the first content initially. */
  defaultOpen?: boolean;
  /** Controlled state: true opens the first content, false closes it. Hydrates a small bridge. */
  open?: boolean;
  /** Called with the new state when the overlay opens or closes. Hydrates a small bridge. */
  onOpenChange?: (open: boolean) => void;
  /** Element id: a handle for `openOverlay('#id')` / `closeOverlay('#id')`. */
  id?: string;
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** Popover. Zero JS via a context-provided CSS scope. */
export function Popover(props: PopoverProps) {
  const open = createCSSGroup<string>(null, { as: 'popover', label: props.label });
  return (
    <PopoverCtx.Provider value={open}>
      <div class={"krate-popover" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} id={props.id}>
        {props.open !== undefined || props.onOpenChange ? <OverlayBridge open={props.open} onOpenChange={props.onOpenChange} /> : null}
        {props.children}
      </div>
    </PopoverCtx.Provider>
  );
}

export interface PopoverTriggerProps {
  value?: string;
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Popover.Trigger` instead. */
export function PopoverTrigger(props: PopoverTriggerProps) {
  const open = PopoverCtx.use();
  return (
    <button class={"krate-popover-trigger" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} type="button" {...open.trigger(props.value)}>
      {props.children}
    </button>
  );
}

export interface PopoverContentProps {
  value?: string;
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Popover.Content` instead. */
export function PopoverContent(props: PopoverContentProps) {
  const open = PopoverCtx.use();
  return (
    <div class={"krate-popover-content" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} {...open.panel(props.value)}>
      {props.children}
    </div>
  );
}

export interface PopoverCloseProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Popover.Close` instead. */
export function PopoverClose(props: PopoverCloseProps) {
  const open = PopoverCtx.use();
  return (
    <button class={"krate-popover-close" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} type="button" aria-label="Close" {...open.close()}>
      {props.children}
    </button>
  );
}

export interface Popover {
  Trigger: typeof PopoverTrigger;
  Content: typeof PopoverContent;
  Close: typeof PopoverClose;
}

Popover.Trigger = PopoverTrigger;
Popover.Content = PopoverContent;
Popover.Close = PopoverClose;
