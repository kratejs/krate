import './sheet.css';
import { createScope, createScopeFlags, createCSSGroup, type CSSScope } from '@krate/runtime';
import { OverlayBridge } from '../overlay/overlay-bridge';

const SheetCtx = createScope<string | null>();

export interface SheetProps {
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

/** Edge-anchored dialog (drawer). Zero JS via a context-provided CSS scope. */
export function Sheet(props: SheetProps) {
  const open = createCSSGroup<string>(null, { as: 'dialog', label: props.label });
  return (
    <SheetCtx.Provider value={open}>
      <div class={"krate-sheet" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} id={props.id}>
        {props.open !== undefined || props.onOpenChange ? <OverlayBridge open={props.open} onOpenChange={props.onOpenChange} /> : null}
        {props.children}
      </div>
    </SheetCtx.Provider>
  );
}

export interface SheetTriggerProps {
  value?: string;
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Sheet.Trigger` instead. */
export function SheetTrigger(props: SheetTriggerProps) {
  const open = SheetCtx.use();
  return (
    <button class={"krate-sheet-trigger" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} type="button" {...open.trigger(props.value)}>
      {props.children}
    </button>
  );
}

export interface SheetContentProps {
  value?: string;
  side?: 'top' | 'right' | 'bottom' | 'left';
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Sheet.Content` instead. */
export function SheetContent(props: SheetContentProps) {
  const open = SheetCtx.use();
  return (
    <div class={"krate-sheet-content" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} role="dialog" data-side={props.side || 'right'} {...open.panel(props.value)}>
      {props.children}
    </div>
  );
}

export interface SheetTitleProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Sheet.Title` instead. */
export function SheetTitle(props: SheetTitleProps) {
  return <h2 class={"krate-sheet-title" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} data-ka="title">{props.children}</h2>;
}

export interface SheetDescriptionProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Sheet.Description` instead. */
export function SheetDescription(props: SheetDescriptionProps) {
  return <p class={"krate-sheet-description" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} data-ka="desc">{props.children}</p>;
}

export interface SheetCloseProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Sheet.Close` instead. */
export function SheetClose(props: SheetCloseProps) {
  const open = SheetCtx.use();
  return (
    <button class={"krate-sheet-close" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} type="button" aria-label="Close" {...open.close()}>
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
