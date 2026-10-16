import './dialog.css';
import { createScope, createScopeFlags, createCSSGroup, type CSSScope } from '@krate/runtime';

const DialogCtx = createScope<string | null>();

export interface DialogProps {
  label?: string;
  children?: any;
}

/** Modal dialog. Zero JS via a context-provided CSS scope (`as: 'dialog'`). */
export function Dialog(props: DialogProps) {
  const open = createCSSGroup<string>(null, { as: 'dialog', label: props.label });
  return (
    <DialogCtx.Provider value={open}>
      <div class="krate-dialog">{props.children}</div>
    </DialogCtx.Provider>
  );
}

export interface DialogTriggerProps {
  value?: string;
  children?: any;
}

/** @deprecated Use `Dialog.Trigger` instead. */
export function DialogTrigger(props: DialogTriggerProps) {
  const open = DialogCtx.use();
  return (
    <button class="krate-dialog-trigger" type="button" {...open.trigger(props.value)}>
      {props.children}
    </button>
  );
}

export interface DialogContentProps {
  value?: string;
  children?: any;
}

/** @deprecated Use `Dialog.Content` instead. */
export function DialogContent(props: DialogContentProps) {
  const open = DialogCtx.use();
  return (
    <div class="krate-dialog-content" role="dialog" {...open.panel(props.value)}>
      {props.children}
    </div>
  );
}

export interface DialogTitleProps {
  children?: any;
}

/** @deprecated Use `Dialog.Title` instead. */
export function DialogTitle(props: DialogTitleProps) {
  return <h2 class="krate-dialog-title">{props.children}</h2>;
}

export interface DialogDescriptionProps {
  children?: any;
}

/** @deprecated Use `Dialog.Description` instead. */
export function DialogDescription(props: DialogDescriptionProps) {
  return <p class="krate-dialog-description">{props.children}</p>;
}

export interface DialogCloseProps {
  children?: any;
}

/** @deprecated Use `Dialog.Close` instead. */
export function DialogClose(props: DialogCloseProps) {
  const open = DialogCtx.use();
  return (
    <button class="krate-dialog-close" type="button" aria-label="Close" {...open.close()}>
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
