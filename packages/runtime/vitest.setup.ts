import { beforeAll } from 'vitest';

// Keep tests off the network. happy-dom loads <link rel="stylesheet"> and
// <script src> for real, so a test that appends one would hit
// http://localhost:3000 (ECONNREFUSED) and fire a spurious error event at a
// random time. Asset requests stay pending until the test dispatches
// load/error by hand; any other request that reaches happy-dom's own fetch is
// a bug in the test and fails loudly.
beforeAll(() => {
  const win = (globalThis as any).happyDOM ?? (globalThis as any).window?.happyDOM;
  if (!win) return;
  win.settings.fetch.interceptor = {
    beforeAsyncRequest: ({ request }: { request: Request }) => {
      const path = new URL(request.url).pathname;
      if (/\.(css|js|mjs)$/.test(path)) return new Promise<Response>(() => {});
      return Promise.reject(new Error(`unexpected network request in test: ${request.method} ${request.url}`));
    },
  };
});
