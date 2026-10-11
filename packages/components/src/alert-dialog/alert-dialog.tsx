import './alert-dialog.css';
import { createScope, createScopeFlags, createCSSGroup, type CSSScope } from '@krate/runtime';

const AlertDialogCtx = createScope<string | null>();

export interface AlertDialogProps {
  label?: string;
  children?: any;
}

/** Confirmation dialog. Zero JS via a context-provided CSS scope (`as: 'modal'`). */
export function AlertDialog(props: AlertDialogProps) {
  const open = createCSSGroup<string>(null, { as: 'modal', label: props.label });
  return (
    <AlertDialogCtx.Provider value={open}>
      <div class="krate-alert-dialog">{props.children}</div>
    </AlertDialogCtx.Provider>
  );
}

export interface AlertDialogTriggerProps {
  value?: string;
  children?: any;
}

/** @deprecated Use `AlertDialog.Trigger` instead. */
export function AlertDialogTrigger(props: AlertDialogTriggerProps) {
  const open = AlertDialogCtx.use();
  return (
    <button class="krate-alert-dialog-trigger" type="button" {...open.trigger(props.value)}>
      {props.children}
    </button>
  );
}

export interface AlertDialogContentProps {
  value?: string;
  children?: any;
}

/** @deprecated Use `AlertDialog.Content` instead. */
export function AlertDialogContent(props: AlertDialogContentProps) {
  const open = AlertDialogCtx.use();
  return (
    <div class="krate-alert-dialog-content" role="alertdialog" {...open.panel(props.value)}>
      {props.children}
    </div>
  );
}

export interface AlertDialogTitleProps {
  children?: any;
}

/** @deprecated Use `AlertDialog.Title` instead. */
export function AlertDialogTitle(props: AlertDialogTitleProps) {
  return <h2 class="krate-alert-dialog-title">{props.children}</h2>;
}

export interface AlertDialogDescriptionProps {
  children?: any;
}

/** @deprecated Use `AlertDialog.Description` instead. */
export function AlertDialogDescription(props: AlertDialogDescriptionProps) {
  return <p class="krate-alert-dialog-description">{props.children}</p>;
}

export interface AlertDialogActionProps {
  children?: any;
}

/** @deprecated Use `AlertDialog.Action` instead. */
export function AlertDialogAction(props: AlertDialogActionProps) {
  const open = AlertDialogCtx.use();
  return (
    <button class="krate-alert-dialog-action" type="button" {...open.close()}>
      {props.children}
    </button>
  );
}

export interface AlertDialogCancelProps {
  children?: any;
}

/** @deprecated Use `AlertDialog.Cancel` instead. */
export function AlertDialogCancel(props: AlertDialogCancelProps) {
  const open = AlertDialogCtx.use();
  return (
    <button class="krate-alert-dialog-cancel" type="button" {...open.close()}>
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
