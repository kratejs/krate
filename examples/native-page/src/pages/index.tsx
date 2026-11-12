import { createEffect, createSignal } from '@krate/runtime';
import { capture, find, onFind, printToPdf, setZoom, stopFind, zoom, type FindEvent } from '@krate/app/page';
import * as downloads from '@krate/app/downloads';
import * as cookies from '@krate/app/cookies';
import { appDataDir } from '@krate/app/os';
import { readText } from '@krate/app/fs';

// One run through the web contents controls: zoom, find, capture, PDF output,
// a download into a folder and cookies. Every step records its outcome in
// globalThis.__pageFlow, which the end-to-end test reads through the shell's
// dev control channel.
type Step = { name: string; ok: boolean; detail: string };
declare global {
  // eslint-disable-next-line no-var
  var __pageFlow: { done: boolean; steps: Step[] };
}

export default function Home() {
  const [status, setStatus] = createSignal('running...');

  createEffect(() => {
    globalThis.__pageFlow = { done: false, steps: [] };
    run()
      .catch((e) => record('flow', false, String(e)))
      .finally(() => {
        globalThis.__pageFlow.done = true;
        setStatus('done');
      });
  });

  return (
    <main>
      <h1>Native Page</h1>
      <p id="status">{status()}</p>
      <p id="text">foo bar Foo baz foo</p>
    </main>
  );
}

function record(name: string, ok: boolean, detail: string) {
  globalThis.__pageFlow.steps.push({ name, ok, detail });
}

async function step(name: string, fn: () => Promise<string>) {
  try {
    record(name, true, await fn());
  } catch (e) {
    record(name, false, e instanceof Error ? `${(e as { code?: string }).code ?? ''} ${e.message} ${(e as { data?: unknown }).data ? JSON.stringify((e as { data?: unknown }).data) : ''}` : String(e));
  }
}

function wait<T>(subscribe: (done: (v: T) => void) => () => void, ms = 15000): Promise<T> {
  return new Promise((resolve, reject) => {
    let stop = () => {};
    const timer = setTimeout(() => {
      stop();
      reject(new Error('timed out'));
    }, ms);
    stop = subscribe((v) => {
      clearTimeout(timer);
      stop();
      resolve(v);
    });
  });
}

async function run() {
  await step('zoom', async () => {
    const before = await zoom();
    await setZoom(1.5);
    const after = await zoom();
    await setZoom(1);
    if (Math.abs(after - 1.5) > 0.01) throw new Error(`zoom is ${after} after setZoom(1.5)`);
    return `${before} -> ${after}`;
  });

  await step('find', async () => {
    const first = wait<FindEvent>((done) => onFind((e) => e.final && done(e)));
    await new Promise((r) => setTimeout(r, 300)); // let the subscription open
    await find('foo');
    const e = await first;
    await stopFind();
    if (e.matches !== 3) throw new Error(`found ${e.matches} matches, want 3`);
    return `${e.matches} matches`;
  });

  await step('capture', async () => {
    const png = await capture();
    const sig = [0x89, 0x50, 0x4e, 0x47];
    if (png.length < 100 || sig.some((b, i) => png[i] !== b)) throw new Error(`not a PNG (${png.length} bytes)`);
    return `${png.length} bytes`;
  });

  await step('pdf', async () => {
    const pdf = await printToPdf({ pageSize: 'Letter', printBackground: true });
    const head = String.fromCharCode(...pdf.slice(0, 4));
    if (head !== '%PDF') throw new Error(`not a PDF (${head})`);
    return `${pdf.length} bytes`;
  });

  await step('download', async () => {
    const dir = (await appDataDir()) + '/downloads';
    await downloads.configure({ dir });
    const done = wait<downloads.DownloadInfo>((fn) => downloads.onDone(fn));
    await new Promise((r) => setTimeout(r, 300));
    const a = document.createElement('a');
    a.href = URL.createObjectURL(new Blob(['hello from the page'], { type: 'text/plain' }));
    a.download = 'page.txt';
    document.body.appendChild(a);
    a.click();
    const info = await done;
    const text = await readText(info.path);
    if (text !== 'hello from the page') throw new Error(`file holds ${JSON.stringify(text)}`);
    return info.path;
  });

  await step('cookies', async () => {
    const url = 'https://example.com';
    await cookies.set({ name: 'flow', value: 'one', httpOnly: true }, url);
    const c = await cookies.get('flow', url);
    if (!c || c.value !== 'one' || !c.httpOnly) throw new Error(`cookie is ${JSON.stringify(c)}`);
    await cookies.delete('flow', url);
    if (await cookies.get('flow', url)) throw new Error('the cookie survived delete');
    await cookies.clearSiteData({ storage: true });
    return 'round trip ok';
  });

  await step('app-origin cookies', async () => {
    // Windows has cookies on its https app origin; WebKit has none for the
    // custom scheme and says so.
    try {
      await cookies.set({ name: 'app', value: '1' });
      await cookies.delete('app');
      return 'supported';
    } catch (e) {
      if ((e as { code?: string }).code === 'unsupported') return 'unsupported';
      throw e;
    }
  });
}
