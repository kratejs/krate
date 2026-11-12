import { createEffect } from '@krate/runtime';
import { main } from '@krate/app/window';
import { createChannel, transferPort } from '@krate/app/port';
import { startWorker } from '@krate/app/worker';
import { openOffscreen, captureFrame, frames } from '@krate/app/offscreen';
import { automationEndpoint, cdp } from '@krate/app/automation';
import { declaredExtensions, listExtensions } from '@krate/app/extension';

function once<T>(register: (resolve: (v: T) => void) => void, ms = 20000): Promise<T> {
  return new Promise((resolve, reject) => {
    const t = setTimeout(() => reject(new Error('timed out')), ms);
    register((v) => {
      clearTimeout(t);
      resolve(v);
    });
  });
}

export default function Home() {
  createEffect(() => {
    run().catch((e) => {
      (document.getElementById('result') as HTMLElement).textContent = 'ERROR ' + (e instanceof Error ? e.message : String(e));
    });
  });

  async function run() {
    const out: Record<string, unknown> = {};
    const mark = (s: string) => {
      (globalThis as unknown as { __step: string }).__step = s;
    };

    mark('worker');
    // A worker process: post a message, read the echo.
    const worker = await startWorker('echo');
    const echoed = once<unknown>((resolve) => {
      worker.onmessage = (d) => {
        if ((d as { echo?: unknown }).echo) resolve(d);
      };
    });
    await worker.postMessage({ op: 'echo', n: 7 });
    out.worker = await echoed;

    mark('view-port');
    // Ports between two views: the peer answers on the end it was handed.
    const win = main();
    const peer = await win.addView({ id: 'peer', path: '/peer', side: 'right', size: 0.3 });
    const { port1, port2 } = await createChannel();
    await transferPort(port2, 'peer');
    await peer.post({ port: port2.id });
    const pong = once<unknown>((resolve) => {
      port1.onmessage = (e) => resolve(e.data);
    });
    // The peer may not have loaded yet: send until it answers.
    for (let i = 0; i < 40 && !out.pong; i++) {
      await peer.post({ port: port2.id });
      await port1.postMessage('ping');
      out.pong = await Promise.race([pong, new Promise((r) => setTimeout(() => r(undefined), 500))]);
    }

    mark('worker-port');
    // A port handed to the worker.
    const wp = await createChannel();
    await transferPort(wp.port2, worker.portOwner);
    const listening = once<unknown>((resolve) => {
      const prev = worker.onmessage;
      worker.onmessage = (d) => {
        prev?.(d);
        if ((d as { listening?: boolean }).listening) resolve(d);
      };
    });
    await worker.postMessage({ op: 'listen', port: wp.port2.id });
    await listening;
    const reply = once<unknown>((resolve) => {
      wp.port1.onmessage = (e) => resolve(e.data);
    });
    await wp.port1.postMessage('hello worker');
    out.workerPort = await reply;

    mark('offscreen');
    // Offscreen frames.
    const off = await openOffscreen({ path: '/color', width: 320, height: 200 });
    await new Promise((r) => setTimeout(r, 1500));
    const png = await captureFrame(off, { format: 'png' });
    out.frame = { w: png.width, h: png.height, png: Array.from(png.data.slice(1, 4)) };
    const seqs: number[] = [];
    for await (const f of frames(off, { fps: 20, maxFrames: 3, format: 'bgra' })) {
      seqs.push(f.seq);
      // The middle pixel of a red page: B, G, R, A.
      const i = ((f.height >> 1) * f.width + (f.width >> 1)) * 4;
      out.pixel = Array.from(f.data.slice(i, i + 4));
    }
    out.seqs = seqs;

    mark('automation');
    // Automation and extensions: report what the platform offers.
    const ep = await automationEndpoint();
    out.endpoint = { enabled: ep.enabled, kind: ep.kind, cdp: ep.cdp };
    try {
      await cdp('Browser.getVersion');
      out.cdp = 'ok';
    } catch (e) {
      out.cdp = (e as { code?: string }).code ?? String(e);
    }
    out.declared = await declaredExtensions();
    try {
      await listExtensions();
      out.extensions = 'ok';
    } catch (e) {
      out.extensions = (e as { code?: string }).code ?? String(e);
    }
    (document.getElementById('result') as HTMLElement).textContent = JSON.stringify(out);
  }

  return (
    <main>
      <h1>Native Procs</h1>
      <p id="progress"></p>
      <p id="result"></p>
    </main>
  );
}
