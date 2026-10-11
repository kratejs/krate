import './popover.css';
import { createScope, createScopeFlags, createCSSGroup, type CSSScope } from '@krate/runtime';

const PopoverCtx = createScope<string | null>();

export interface PopoverProps {
  label?: string;
  children?: any;
}

/** Popover. Zero JS via a context-provided CSS scope. */
export function Popover(props: PopoverProps) {
  const open = createCSSGroup<string>(null, { as: 'popover', label: props.label });
  return (
    <PopoverCtx.Provider value={open}>
      <div class="krate-popover">{props.children}</div>
    </PopoverCtx.Provider>
  );
}

export interface PopoverTriggerProps {
  value?: string;
  children?: any;
}

/** @deprecated Use `Popover.Trigger` instead. */
export function PopoverTrigger(props: PopoverTriggerProps) {
  const open = PopoverCtx.use();
  return (
    <button class="krate-popover-trigger" type="button" {...open.trigger(props.value)}>
      {props.children}
    </button>
  );
}

export interface PopoverContentProps {
  value?: string;
  children?: any;
}

/** @deprecated Use `Popover.Content` instead. */
export function PopoverContent(props: PopoverContentProps) {
  const open = PopoverCtx.use();
  return (
    <div class="krate-popover-content" {...open.panel(props.value)}>
      {props.children}
    </div>
  );
}

export interface PopoverCloseProps {
  children?: any;
}

/** @deprecated Use `Popover.Close` instead. */
export function PopoverClose(props: PopoverCloseProps) {
  const open = PopoverCtx.use();
  return (
    <button class="krate-popover-close" type="button" aria-label="Close" {...open.close()}>
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
