import './dialog.css';
import { createScope, createScopeFlags, createCSSGroup, type CSSScope } from '@krate/runtime';
import { OverlayBridge } from '../overlay/overlay-bridge';

const DialogCtx = createScope<string | null>();

export interface DialogProps {
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

/** Modal dialog. Zero JS via a context-provided CSS scope (`as: 'dialog'`). */
export function Dialog(props: DialogProps) {
  const open = createCSSGroup<string>(null, { as: 'dialog', label: props.label });
  return (
    <DialogCtx.Provider value={open}>
      <div class={"krate-dialog" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} id={props.id}>
        {props.open !== undefined || props.onOpenChange ? <OverlayBridge open={props.open} onOpenChange={props.onOpenChange} /> : null}
        {props.children}
      </div>
    </DialogCtx.Provider>
  );
}

export interface DialogTriggerProps {
  value?: string;
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Dialog.Trigger` instead. */
export function DialogTrigger(props: DialogTriggerProps) {
  const open = DialogCtx.use();
  return (
    <button class={"krate-dialog-trigger" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} type="button" {...open.trigger(props.value)}>
      {props.children}
    </button>
  );
}

export interface DialogContentProps {
  value?: string;
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Dialog.Content` instead. */
export function DialogContent(props: DialogContentProps) {
  const open = DialogCtx.use();
  return (
    <div class={"krate-dialog-content" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} role="dialog" {...open.panel(props.value)}>
      {props.children}
    </div>
  );
}

export interface DialogTitleProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Dialog.Title` instead. */
export function DialogTitle(props: DialogTitleProps) {
  return <h2 class={"krate-dialog-title" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} data-ka="title">{props.children}</h2>;
}

export interface DialogDescriptionProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Dialog.Description` instead. */
export function DialogDescription(props: DialogDescriptionProps) {
  return <p class={"krate-dialog-description" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} data-ka="desc">{props.children}</p>;
}

export interface DialogCloseProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Dialog.Close` instead. */
export function DialogClose(props: DialogCloseProps) {
  const open = DialogCtx.use();
  return (
    <button class={"krate-dialog-close" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} type="button" aria-label="Close" {...open.close()}>
      {props.children}
    </button>
  );
}

export interface Dialog {
  Trigger: typeof DialogTrigger;
  Content: typeof DialogContent;
  Title: typeof DialogTitle;
  Description: typeof DialogDescription;
  Close: typeof DialogClose;
}

Dialog.Trigger = DialogTrigger;
Dialog.Content = DialogContent;
Dialog.Title = DialogTitle;
Dialog.Description = DialogDescription;
Dialog.Close = DialogClose;
