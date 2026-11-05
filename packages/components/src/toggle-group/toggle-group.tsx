import './toggle-group.css';
import { createScope, createScopeFlags, createCSSChoice, createCSSFlags, type CSSScope, type CSSFlagsScope } from '@krate/runtime';

const ToggleGroupCtx = createScope<string>();
const ToggleGroupMultiCtx = createScopeFlags<string>();

export interface ToggleGroupProps {
  defaultValue?: string;
  label?: string;
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** Single-select toggle group (radio semantics), zero JS. */
export function ToggleGroup(props: ToggleGroupProps) {
  const value = createCSSChoice<string>('', { as: 'radiogroup', label: props.label });
  return (
    <ToggleGroupCtx.Provider value={value}>
      <div class={"krate-toggle-group" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}>{props.children}</div>
    </ToggleGroupCtx.Provider>
  );
}

export interface ToggleGroupItemProps {
  value?: string;
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `ToggleGroup.Item` instead. */
export function ToggleGroupItem(props: ToggleGroupItemProps) {
  const value = ToggleGroupCtx.use();
  return (
    <button class={"krate-toggle-group-item" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} type="button" {...value.trigger(props.value)}>
      {props.children}
    </button>
  );
}

/** Multi-select toggle group (checkbox semantics), zero JS. */
export function ToggleGroupMultiple(props: ToggleGroupProps) {
  const flags = createCSSFlags<string>([], { label: props.label });
  return (
    <ToggleGroupMultiCtx.Provider value={flags}>
      <div class={"krate-toggle-group" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}>{props.children}</div>
    </ToggleGroupMultiCtx.Provider>
  );
}

/** @deprecated Use `ToggleGroupMultiple.Item` instead. */
export function ToggleGroupMultipleItem(props: ToggleGroupItemProps) {
  const flags = ToggleGroupMultiCtx.use();
  return (
    <button class={"krate-toggle-group-item" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} type="button" {...flags.trigger(props.value as string)}>
      {props.children}
    </button>
  );
}

export interface ToggleGroup {
  Item: typeof ToggleGroupItem;
}

ToggleGroup.Item = ToggleGroupItem;

export interface ToggleGroupMultiple {
  Item: typeof ToggleGroupMultipleItem;
}

ToggleGroupMultiple.Item = ToggleGroupMultipleItem;
