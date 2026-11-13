import { createSignal, createEffect } from '@krate/runtime';
import { getPolicy, decisions, osStatus } from '@krate/app/media';
import { query } from '@krate/app/permission';

// The page asks the webview for the camera, the microphone and the location.
// The shell answers from native.media, and the page reports what each call did.
async function attempt(fn: () => Promise<unknown>): Promise<string> {
  try {
    await fn();
    return 'ok';
  } catch (e) {
    const err = e as { name?: string; code?: number };
    return err.name && err.name !== 'Error' ? err.name : 'code' + String(err.code);
  }
}

const stop = (s: MediaStream) => s.getTracks().forEach((t) => t.stop());

async function run(): Promise<string> {
  const out: Record<string, string> = {};
  out.camera = await attempt(() => navigator.mediaDevices.getUserMedia({ video: true }).then(stop));
  out.microphone = await attempt(() => navigator.mediaDevices.getUserMedia({ audio: true }).then(stop));
  out.location = await attempt(
    () =>
      new Promise((ok, no) =>
        navigator.geolocation.getCurrentPosition(ok, (e) => no({ code: e.code }), { timeout: 4000 }),
      ),
  );
  out.mediaPermission = String(await query('media'));
  out.locationPermission = String(await query('location'));
  out.policy = JSON.stringify(await getPolicy());
  out.decisions = String((await decisions()).length);
  out.osCamera = await osStatus('camera');
  return JSON.stringify(out);
}

export default function Home() {
  const [result, setResult] = createSignal('running...');

  createEffect(() => {
    run()
      .then(setResult)
      .catch((e) => setResult('failed: ' + (e instanceof Error ? e.message : String(e))));
  });

  return (
    <main>
      <h1>Native Media</h1>
      <p id="result">{result()}</p>
    </main>
  );
}
