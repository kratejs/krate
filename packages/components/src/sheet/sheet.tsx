import './sheet.css';
import { createScope, createScopeFlags, createCSSGroup, type CSSScope } from '@krate/runtime';

const SheetCtx = createScope<string | null>();

export interface SheetProps {
  label?: string;
  children?: any;
}

/** Edge-anchored dialog (drawer). Zero JS via a context-provided CSS scope. */
export function Sheet(props: SheetProps) {
  const open = createCSSGroup<string>(null, { as: 'dialog', label: props.label });
  return (
    <SheetCtx.Provider value={open}>
      <div class="krate-sheet">{props.children}</div>
    </SheetCtx.Provider>
  );
}

export interface SheetTriggerProps {
  value?: string;
  children?: any;
}

/** @deprecated Use `Sheet.Trigger` instead. */
export function SheetTrigger(props: SheetTriggerProps) {
  const open = SheetCtx.use();
  return (
    <button class="krate-sheet-trigger" type="button" {...open.trigger(props.value)}>
      {props.children}
    </button>
  );
}

export interface SheetContentProps {
  value?: string;
  side?: 'top' | 'right' | 'bottom' | 'left';
  children?: any;
}

/** @deprecated Use `Sheet.Content` instead. */
export function SheetContent(props: SheetContentProps) {
  const open = SheetCtx.use();
  return (
    <div class="krate-sheet-content" role="dialog" data-side={props.side || 'right'} {...open.panel(props.value)}>
      {props.children}
    </div>
  );
}

export interface SheetTitleProps {
  children?: any;
}

/** @deprecated Use `Sheet.Title` instead. */
export function SheetTitle(props: SheetTitleProps) {
  return <h2 class="krate-sheet-title">{props.children}</h2>;
}

export interface SheetDescriptionProps {
  children?: any;
}

/** @deprecated Use `Sheet.Description` instead. */
export function SheetDescription(props: SheetDescriptionProps) {
  return <p class="krate-sheet-description">{props.children}</p>;
}

export interface SheetCloseProps {
  children?: any;
}

/** @deprecated Use `Sheet.Close` instead. */
export function SheetClose(props: SheetCloseProps) {
  const open = SheetCtx.use();
  return (
    <button class="krate-sheet-close" type="button" aria-label="Close" {...open.close()}>
      {props.children}
    </button>
  );
}

export interface Sheet {
  Trigger: typeof SheetTrigger;
  Content: typeof SheetContent;
  Title: typeof SheetTitle;
  Description: typeof SheetDescription;
  Close: typeof SheetClose;
}

Sheet.Trigger = SheetTrigger;
Sheet.Content = SheetContent;
Sheet.Title = SheetTitle;
Sheet.Description = SheetDescription;
Sheet.Close = SheetClose;
