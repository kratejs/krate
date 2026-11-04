export interface ToastOptions {
  title?: string;
  description?: string;
  variant?: 'default' | 'success' | 'destructive';
  /** Milliseconds before the toast dismisses itself; 0 keeps it until closed. Default 5000. */
  duration?: number;
}

export interface ToastHandle {
  id: string;
  dismiss: () => void;
}

let seq = 0;

function viewport(): HTMLElement {
  let vp = document.querySelector<HTMLElement>('.krate-toaster');
  if (!vp) {
    vp = document.createElement('div');
    vp.className = 'krate-toaster';
    vp.setAttribute('role', 'region');
    vp.setAttribute('aria-label', 'Notifications');
    vp.setAttribute('aria-live', 'polite');
    document.body.appendChild(vp);
  }
  return vp;
}

/**
 * Shows a toast with the same markup and styles as `<Toast>`. Client-only: it
 * throws a clear error during server rendering instead of silently doing
 * nothing.
 */
export function toast(options: ToastOptions | string): ToastHandle {
  if (typeof document === 'undefined') {
    throw new Error('toast() is client-only: call it from an event handler or onMount, not during server rendering');
  }
  const opts: ToastOptions = typeof options === 'string' ? { title: options } : options;
  const id = 'krate-toast-' + ++seq;
  const el = document.createElement('div');
  el.className = 'krate-toast';
  el.id = id;
  el.setAttribute('data-variant', opts.variant || 'default');
  el.setAttribute('role', opts.variant === 'destructive' ? 'alert' : 'status');
  const body = document.createElement('div');
  body.className = 'krate-toast-body';
  if (opts.title) {
    const t = document.createElement('div');
    t.className = 'krate-toast-title';
    t.textContent = opts.title;
    body.appendChild(t);
  }
  if (opts.description) {
    const d = document.createElement('div');
    d.className = 'krate-toast-description';
    d.textContent = opts.description;
    body.appendChild(d);
  }
  const close = document.createElement('button');
  close.type = 'button';
  close.className = 'krate-toast-close';
  close.setAttribute('aria-label', 'Close');
  close.textContent = '×';
  el.appendChild(body);
  el.appendChild(close);
  viewport().appendChild(el);
  let timer: ReturnType<typeof setTimeout> | undefined;
  const dismiss = () => {
    if (timer) clearTimeout(timer);
    el.remove();
  };
  close.addEventListener('click', dismiss);
  const duration = opts.duration === undefined ? 5000 : opts.duration;
  if (duration > 0) timer = setTimeout(dismiss, duration);
  return { id, dismiss };
}
