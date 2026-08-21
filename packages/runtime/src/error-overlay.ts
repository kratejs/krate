/**
 * Dev Error Overlay — shows a styled error overlay in development mode.
 * Catches unhandled errors and unhandled promise rejections,
 * displays them in a dismissable overlay with file/line info.
 */

let overlayVisible = false;

function getOverlayStyle(): string {
  return `
    position:fixed;top:0;left:0;right:0;bottom:0;z-index:99999;
    background:rgba(0,0,0,0.85);color:#fff;font-family:Menlo,Monaco,"Courier New",monospace;
    display:flex;align-items:center;justify-content:center;backdrop-filter:blur(4px);
  `;
}

function getCardStyle(): string {
  return `
    background:#1a1a2e;border:1px solid #e74c3c;border-radius:8px;
    padding:24px 32px;max-width:800px;max-height:80vh;overflow:auto;
    box-shadow:0 8px 32px rgba(231,76,60,0.3);width:90%;
  `;
}

function getTitleStyle(): string {
  return `color:#e74c3c;font-size:18px;font-weight:bold;margin-bottom:12px;`;
}

function getMessageStyle(): string {
  return `color:#ecf0f1;font-size:14px;line-height:1.6;white-space:pre-wrap;word-break:break-word;`;
}

function getStackStyle(): string {
  return `color:#95a5a6;font-size:12px;line-height:1.5;margin-top:12px;white-space:pre-wrap;`;
}

function getCloseBtnStyle(): string {
  return `
    position:absolute;top:16px;right:16px;background:#e74c3c;color:#fff;
    border:none;border-radius:4px;padding:6px 12px;cursor:pointer;font-size:14px;
    font-family:inherit;
  `;
}

function getSourceLinkStyle(): string {
  return `color:#3498db;font-size:12px;margin-top:8px;text-decoration:underline;cursor:pointer;`;
}

function createOverlay(): HTMLElement {
  const overlay = document.createElement('div');
  overlay.id = 'krate-error-overlay';
  overlay.setAttribute('style', getOverlayStyle());
  return overlay;
}

function parseStack(stack: string): { file: string; line: string; col: string; func: string } | null {
  if (!stack) return null;
  // Match patterns like "at functionName (file:line:col)" or "at file:line:col"
  const match = stack.match(/at\s+(?:(\S+)\s+\()?(.+?):(\d+):(\d+)/);
  if (match) {
    return { func: match[1] || '<anonymous>', file: match[2], line: match[3], col: match[4] };
  }
  return null;
}

function showError(title: string, message: string, stack?: string): void {
  if (overlayVisible) return;
  overlayVisible = true;

  const overlay = createOverlay();

  const source = stack ? parseStack(stack) : null;
  const sourceLink = source
    ? `<div style="${getSourceLinkStyle()}" onclick="navigator.clipboard.writeText('${source.file}:${source.line}:${source.col}')">📋 ${source.file}:${source.line}:${source.col}</div>`
    : '';

  overlay.innerHTML = `
    <div style="${getCardStyle()};position:relative;">
      <button style="${getCloseBtnStyle()}" onclick="document.getElementById('krate-error-overlay').remove();window.__krate_overlay_visible=false;">✕ Dismiss</button>
      <div style="${getTitleStyle()}">⚡ ${title}</div>
      <div style="${getMessageStyle()}">${escapeHTML(message)}</div>
      ${sourceLink}
      ${stack ? `<div style="${getStackStyle()}">${escapeHTML(stack)}</div>` : ''}
      <div style="color:#7f8c8d;font-size:11px;margin-top:16px;">Press Esc to dismiss · Edit the file to fix, then the page will auto-reload</div>
    </div>
  `;

  document.body.appendChild(overlay);

  // Dismiss on Escape
  const escHandler = (e: KeyboardEvent) => {
    if (e.key === 'Escape' && overlay.parentNode) {
      overlay.remove();
      overlayVisible = false;
      document.removeEventListener('keydown', escHandler);
    }
  };
  document.addEventListener('keydown', escHandler);

  // Auto-dismiss on successful reload (beforeunload)
  window.addEventListener('beforeunload', () => {
    if (overlay.parentNode) overlay.remove();
    overlayVisible = false;
  });
}

function escapeHTML(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

export function initErrorOverlay(): void {
  if (typeof window === 'undefined') return;

  window.addEventListener('error', (e: ErrorEvent) => {
    e.preventDefault();
    showError(
      'Runtime Error',
      e.message || 'An unknown error occurred',
      e.error?.stack || ''
    );
  });

  window.addEventListener('unhandledrejection', (e: PromiseRejectionEvent) => {
    e.preventDefault();
    const reason = e.reason;
    const message = reason instanceof Error ? reason.message : String(reason);
    const stack = reason instanceof Error ? reason.stack : '';
    showError('Unhandled Promise Rejection', message, stack || '');
  });
}
