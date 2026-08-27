import './toast.css';
import { createSignal, onCleanup } from '@krate/runtime';

export interface ToastProps {
  children?: any;
  variant?: 'default' | 'success' | 'destructive';
  duration?: number;
  onClose?: () => void;
}

export function Toast(props: ToastProps) {
  var [visible, setVisible] = createSignal(true);

  function handleClose() {
    setVisible(false);
    if (props.onClose) {
      props.onClose();
    }
  }

  return (
    <div
      class={"krate-toast krate-toast-" + (props.variant || "default")}
      data-state={visible() ? "open" : "closed"}
      role="status"
    >
      <div class="krate-toast-content">
        {props.children}
      </div>
      <button class="krate-toast-close" type="button" aria-label="Close" onClick={handleClose}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M18 6 6 18"/><path d="m6 6 12 12"/>
        </svg>
      </button>
    </div>
  );
}

export interface ToastProviderProps {
  children?: any;
  swipeDirection?: 'left' | 'right' | 'up' | 'down';
}

export function ToastProvider(props: ToastProviderProps) {
  return (
    <div class="krate-toast-provider">
      {props.children}
    </div>
  );
}

export interface ToastViewportProps {
}

export function ToastViewport(props: ToastViewportProps) {
  return (
    <div class="krate-toast-viewport">
      {props.children}
    </div>
  );
}

export interface ToastActionProps {
  children?: any;
  altText?: string;
}

export function ToastAction(props: ToastActionProps) {
  return (
    <button class="krate-toast-action" type="button">
      {props.children}
    </button>
  );
}

export interface ToastTitleProps {
  children?: any;
}

export function ToastTitle(props: ToastTitleProps) {
  return <div class="krate-toast-title">{props.children}</div>;
}

export interface ToastDescriptionProps {
  children?: any;
}

export function ToastDescription(props: ToastDescriptionProps) {
  return <div class="krate-toast-description">{props.children}</div>;
}
