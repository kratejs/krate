import './accordion.css';
import { createScope, createScopeFlags, createCSSGroup, createCSSFlags, type CSSScope, type CSSFlagsScope } from '@krate/runtime';

const AccordionCtx = createScope<string | null>();
const AccordionMultiCtx = createScopeFlags<string>();

export interface AccordionProps {
  /** Literal initial open item value. */
  defaultValue?: string;
  label?: string;
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/**
 * Single-open accordion. Zero JS via a CSS scope provided through context.
 *
 * ```tsx
 * <Accordion defaultValue="q1">
 *   <Accordion.Item>
 *     <Accordion.Trigger value="q1">Question</Accordion.Trigger>
 *     <Accordion.Content value="q1">Answer</Accordion.Content>
 *   </Accordion.Item>
 * </Accordion>
 * ```
 */
export function Accordion(props: AccordionProps) {
  const open = createCSSGroup<string>(null, { as: 'accordion', label: props.label });
  return (
    <AccordionCtx.Provider value={open}>
      <div class={"krate-accordion" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}>{props.children}</div>
    </AccordionCtx.Provider>
  );
}

export interface AccordionMultipleProps {
  label?: string;
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** Multiple-open accordion: each item toggles independently (checkboxes). */
export function AccordionMultiple(props: AccordionMultipleProps) {
  const open = createCSSFlags<string>([], { as: 'accordion', label: props.label });
  return (
    <AccordionMultiCtx.Provider value={open}>
      <div class={"krate-accordion" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}>{props.children}</div>
    </AccordionMultiCtx.Provider>
  );
}

export interface AccordionItemProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Accordion.Item` instead. */
/** @deprecated Use `AccordionMultiple.Item` instead. */
export function AccordionItem(props: AccordionItemProps) {
  return <div class={"krate-accordion-item" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}>{props.children}</div>;
}

export interface AccordionTriggerProps {
  value?: string;
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Accordion.Trigger` instead. */
export function AccordionTrigger(props: AccordionTriggerProps) {
  const open = AccordionCtx.use();
  return (
    <button class={"krate-accordion-trigger" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} type="button" {...open.trigger(props.value)}>
      <span class="krate-accordion-trigger-text">{props.children}</span>
      <span class="krate-accordion-trigger-icon" aria-hidden="true">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="m6 9 6 6 6-6" />
        </svg>
      </span>
    </button>
  );
}

/** @deprecated Use `AccordionMultiple.Trigger` instead. */
export function AccordionMultipleTrigger(props: AccordionTriggerProps) {
  const open = AccordionMultiCtx.use();
  return (
    <button class={"krate-accordion-trigger" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} type="button" {...open.trigger(props.value as string)}>
      <span class="krate-accordion-trigger-text">{props.children}</span>
      <span class="krate-accordion-trigger-icon" aria-hidden="true">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="m6 9 6 6 6-6" />
        </svg>
      </span>
    </button>
  );
}

export interface AccordionContentProps {
  value?: string;
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Accordion.Content` instead. */
export function AccordionContent(props: AccordionContentProps) {
  const open = AccordionCtx.use();
  return (
    <div class={"krate-accordion-content" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} {...open.panel(props.value)}>
      <div class="krate-accordion-content-inner">{props.children}</div>
    </div>
  );
}

/** @deprecated Use `AccordionMultiple.Content` instead. */
export function AccordionMultipleContent(props: AccordionContentProps) {
  const open = AccordionMultiCtx.use();
  return (
    <div class={"krate-accordion-content" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} {...open.panel(props.value as string)}>
      <div class="krate-accordion-content-inner">{props.children}</div>
    </div>
  );
}

export interface Accordion {
  Item: typeof AccordionItem;
  Trigger: typeof AccordionTrigger;
  Content: typeof AccordionContent;
}

Accordion.Item = AccordionItem;
Accordion.Trigger = AccordionTrigger;
Accordion.Content = AccordionContent;

export interface AccordionMultiple {
  Item: typeof AccordionItem;
  Trigger: typeof AccordionMultipleTrigger;
  Content: typeof AccordionMultipleContent;
}

AccordionMultiple.Item = AccordionItem;
AccordionMultiple.Trigger = AccordionMultipleTrigger;
AccordionMultiple.Content = AccordionMultipleContent;
