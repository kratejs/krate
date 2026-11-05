import './alert-dialog.css';
import { createScope, createScopeFlags, createCSSGroup, type CSSScope } from '@krate/runtime';
import { OverlayBridge } from '../overlay/overlay-bridge';

const AlertDialogCtx = createScope<string | null>();

export interface AlertDialogProps {
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

/** Confirmation dialog. Zero JS via a context-provided CSS scope (`as: 'modal'`). */
export function AlertDialog(props: AlertDialogProps) {
  const open = createCSSGroup<string>(null, { as: 'modal', label: props.label });
  return (
    <AlertDialogCtx.Provider value={open}>
      <div class={"krate-alert-dialog" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} id={props.id}>
        {props.open !== undefined || props.onOpenChange ? <OverlayBridge open={props.open} onOpenChange={props.onOpenChange} /> : null}
        {props.children}
      </div>
    </AlertDialogCtx.Provider>
  );
}

export interface AlertDialogTriggerProps {
  value?: string;
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `AlertDialog.Trigger` instead. */
export function AlertDialogTrigger(props: AlertDialogTriggerProps) {
  const open = AlertDialogCtx.use();
  return (
    <button class={"krate-alert-dialog-trigger" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} type="button" {...open.trigger(props.value)}>
      {props.children}
    </button>
  );
}

export interface AlertDialogContentProps {
  value?: string;
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `AlertDialog.Content` instead. */
export function AlertDialogContent(props: AlertDialogContentProps) {
  const open = AlertDialogCtx.use();
  return (
    <div class={"krate-alert-dialog-content" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} role="alertdialog" {...open.panel(props.value)}>
      {props.children}
    </div>
  );
}

export interface AlertDialogTitleProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `AlertDialog.Title` instead. */
export function AlertDialogTitle(props: AlertDialogTitleProps) {
  return <h2 class={"krate-alert-dialog-title" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} data-ka="title">{props.children}</h2>;
}

export interface AlertDialogDescriptionProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `AlertDialog.Description` instead. */
export function AlertDialogDescription(props: AlertDialogDescriptionProps) {
  return <p class={"krate-alert-dialog-description" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} data-ka="desc">{props.children}</p>;
}

export interface AlertDialogActionProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `AlertDialog.Action` instead. */
export function AlertDialogAction(props: AlertDialogActionProps) {
  const open = AlertDialogCtx.use();
  return (
    <button class={"krate-alert-dialog-action" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} type="button" {...open.close()}>
      {props.children}
    </button>
  );
}

export interface AlertDialogCancelProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `AlertDialog.Cancel` instead. */
export function AlertDialogCancel(props: AlertDialogCancelProps) {
  const open = AlertDialogCtx.use();
  return (
    <button class={"krate-alert-dialog-cancel" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} type="button" {...open.close()}>
      {props.children}
    </button>
  );
}

export interface AlertDialog {
  Trigger: typeof AlertDialogTrigger;
  Content: typeof AlertDialogContent;
  Title: typeof AlertDialogTitle;
  Description: typeof AlertDialogDescription;
  Action: typeof AlertDialogAction;
  Cancel: typeof AlertDialogCancel;
}

AlertDialog.Trigger = AlertDialogTrigger;
AlertDialog.Content = AlertDialogContent;
AlertDialog.Title = AlertDialogTitle;
AlertDialog.Description = AlertDialogDescription;
AlertDialog.Action = AlertDialogAction;
AlertDialog.Cancel = AlertDialogCancel;
