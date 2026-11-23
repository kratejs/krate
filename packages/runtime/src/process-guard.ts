/**
 * Process-level error policy of the SSR renderer.
 *
 * A render error is that request's error: it ends as a 500 or the error page.
 * Node's defaults are harsher for the errors a render leaves behind outside its
 * own promise chain, a rejected promise nobody awaited or a throw from a timer
 * or event callback: they end the process, and every other page then answers
 * 503 until the supervisor has restarted the renderer. Those are logged and the
 * renderer keeps serving. What stays fatal is an uncaught exception that does
 * not belong to any request (the renderer's own code, a failed listener), since
 * the process state can no longer be trusted.
 */

import { getRequestContext } from './request-context.js';

/** The part of `process` the guards use. */
export interface GuardedProcess {
  on(event: 'unhandledRejection' | 'uncaughtException', listener: (...args: any[]) => void): unknown;
  exit(code?: number): unknown;
}

function describe(err: unknown): string {
  if (err instanceof Error) return err.stack || `${err.name}: ${err.message}`;
  try {
    return typeof err === 'string' ? err : JSON.stringify(err);
  } catch {
    return String(err);
  }
}

function where(): string {
  const req = getRequestContext();
  return req ? ` while rendering ${req.method} ${req.url}` : '';
}

/**
 * Installs the policy. An unhandled rejection never ends the renderer. An
 * uncaught exception raised inside a request's async scope is logged; one
 * outside any request exits with status 1, as Node does by default.
 */
export function installProcessGuards(
  proc: GuardedProcess = process,
  log: (...args: unknown[]) => void = console.error,
): void {
  proc.on('unhandledRejection', (reason: unknown) => {
    log(`[krate-ssr] unhandled promise rejection${where()} (the renderer keeps running): ${describe(reason)}`);
  });
  proc.on('uncaughtException', (err: unknown) => {
    if (getRequestContext()) {
      log(`[krate-ssr] uncaught exception${where()} (the renderer keeps running): ${describe(err)}`);
      return;
    }
    log(`[krate-ssr] uncaught exception outside any request, exiting: ${describe(err)}`);
    proc.exit(1);
  });
}
