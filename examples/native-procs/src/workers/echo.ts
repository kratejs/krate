import { onMessage, postMessage } from '@krate/app/worker-host';
import { portFromId } from '@krate/app/port';

// Runs in its own Node process. Messages arrive from the app; postMessage
// sends back. A channel end handed to this worker is used with portFromId.
onMessage(async (m: { op: string; port?: string; n?: number }) => {
  if (m.op === 'echo') {
    await postMessage({ echo: m });
  } else if (m.op === 'listen' && m.port) {
    const port = portFromId(m.port);
    port.onmessage = (e) => {
      void port.postMessage({ workerGot: e.data });
    };
    await postMessage({ listening: true });
  }
});
