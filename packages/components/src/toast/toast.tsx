import './toast.css';
import { createScope, createScopeFlags, createCSSToggle, type CSSScope } from '@krate/runtime';

const ToastCtx = createScope<boolean>();

export interface ToastProps {
  variant?: 'default' | 'success' | 'destructive';
  children?: any;
}

/** Toast. Zero JS: a hidden checkbox shows it; the close button unchecks it. */
export function Toast(props: ToastProps) {
  const open = createCSSToggle(true);
  return (
    <ToastCtx.Provider value={open}>
      <div class="krate-toast" data-variant={props.variant || 'default'} role="status" {...open.panel(true)}>
        <div class="krate-toast-body">{props.children}</div>
        <ToastClose />
      </div>
    </ToastCtx.Provider>
  );
}

/** @deprecated Use `Toast.Close` instead. */
export function ToastClose() {
  const open = ToastCtx.use();
  return (
    <button class="krate-toast-close" type="button" aria-label="Close" {...open.close()}>
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <path d="M18 6 6 18M6 6l12 12" />
      </svg>
    </button>
  );
}

export interface ToastTitleProps {
  children?: any;
}

/** @deprecated Use `Toast.Title` instead. */
export function ToastTitle(props: ToastTitleProps) {
  return <div class="krate-toast-title">{props.children}</div>;
}

export interface ToastDescriptionProps {
  children?: any;
}

/** @deprecated Use `Toast.Description` instead. */
export function ToastDescription(props: ToastDescriptionProps) {
  return <div class="krate-toast-description">{props.children}</div>;
}

export interface Toast {
  Title: typeof ToastTitle;
  Description: typeof ToastDescription;
  Close: typeof ToastClose;
}

Toast.Title = ToastTitle;
Toast.Description = ToastDescription;
Toast.Close = ToastClose;
