import { createEffect } from '@krate/runtime';
import * as badge from '@krate/app/badge';
import * as crash from '@krate/app/crash';
import * as login from '@krate/app/login';
import * as power from '@krate/app/power';
import * as screen from '@krate/app/screen';
import * as theme from '@krate/app/theme';

// Calls every OS integration area once and keeps the outcome of each call in
// globalThis.__sys for the end-to-end test to read. A call that fails keeps
// its error code, so "unsupported" on a machine without a service shows as
// that and not as a page error.
const sys: Record<string, unknown> = ((globalThis as any).__sys = { done: false, events: [] });

async function step(name: string, f: () => Promise<unknown>) {
  try {
    sys[name] = { ok: true, value: await f() };
  } catch (e) {
    sys[name] = { ok: false, code: (e as { code?: string }).code ?? 'error', message: String(e) };
  }
}

async function run() {
  await step('displays', () => screen.listDisplays());
  await step('primary', () => screen.primaryDisplay());
  await step('ofWindow', () => screen.displayOfWindow(''));
  await step('center', () => screen.centerWindow('', ''));
  await step('theme', () => theme.getTheme());
  await step('power', () => power.powerState());
  await step('badge', async () => {
    await badge.setBadge(3);
    const shown = await badge.getBadge();
    await badge.setProgress('normal', 0.5);
    const p = await badge.getProgress();
    await badge.clearBadge();
    await badge.clearProgress();
    return { shown, progress: p.state, after: await badge.getBadge() };
  });
  await step('login', async () => {
    const before = (await login.getLoginItem()).enabled;
    await login.setLoginItem(true, { args: ['--background'] });
    const during = await login.getLoginItem();
    await login.setLoginItem(false);
    return { before, during: during.enabled, method: during.method, after: (await login.getLoginItem()).enabled };
  });
  await step('autoReload', () => crash.setCrashAutoReload(true));
  await step('crashList', () => crash.listCrashReports());
  crash.onCrash((e) => (sys.events as unknown[]).push(e));
  sys.done = true;
}

export default function Home() {
  createEffect(() => {
    void run();
  });
  return (
    <main>
      <h1>Native System</h1>
    </main>
  );
}
