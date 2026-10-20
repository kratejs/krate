export interface NavigateOptions {
  replace?: boolean;
  scroll?: boolean;
  state?: unknown;
}

export function initRouter(): void {}

export function reinitRouter(): void {}

export function navigate(url: string, opts: NavigateOptions = {}): boolean {
  if (typeof location === 'undefined') return true;
  if (opts.replace) location.replace(url);
  else location.assign(url);
  return true;
}