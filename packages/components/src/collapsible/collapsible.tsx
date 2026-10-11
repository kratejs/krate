import './collapsible.css';
import { createScope, createScopeFlags, createCSSGroup, type CSSScope } from '@krate/runtime';

const CollapsibleCtx = createScope<string | null>();

export interface CollapsibleProps {
  /** Literal initial open state value (the trigger/content `value`). */
  defaultValue?: string;
  label?: string;
  children?: any;
}

/**
 * Disclosure (single open/close). Zero JS via a context-provided CSS scope. Use
 * a shared literal `value` on the trigger and content:
 *
 * ```tsx
 * <Collapsible defaultValue="content">
 *   <Collapsible.Trigger value="content">Toggle</Collapsible.Trigger>
 *   <Collapsible.Content value="content">Body</Collapsible.Content>
 * </Collapsible>
 * ```
 */
export function Collapsible(props: CollapsibleProps) {
  const open = createCSSGroup<string>(null, { as: 'disclosure', label: props.label });
  return (
    <CollapsibleCtx.Provider value={open}>
      <div class="krate-collapsible">{props.children}</div>
    </CollapsibleCtx.Provider>
  );
}

export interface CollapsibleTriggerProps {
  value?: string;
  children?: any;
}

/** @deprecated Use `Collapsible.Trigger` instead. */
export function CollapsibleTrigger(props: CollapsibleTriggerProps) {
  const open = CollapsibleCtx.use();
  return (
    <button class="krate-collapsible-trigger" type="button" {...open.trigger(props.value)}>
      {props.children}
    </button>
  );
}

export interface CollapsibleContentProps {
  value?: string;
  children?: any;
}

/** @deprecated Use `Collapsible.Content` instead. */
export function CollapsibleContent(props: CollapsibleContentProps) {
  const open = CollapsibleCtx.use();
  return (
    <div class="krate-collapsible-content" {...open.panel(props.value)}>
      {props.children}
    </div>
  );
}

export interface Collapsible {
  Trigger: typeof CollapsibleTrigger;
  Content: typeof CollapsibleContent;
}

Collapsible.Trigger = CollapsibleTrigger;
Collapsible.Content = CollapsibleContent;
