import { onViewMessage } from '@krate/app/window';
import { portFromId } from '@krate/app/port';

// The peer view is handed one end of a channel and answers on it. The main
// page tells it the end's id with a view message; the messages on the channel
// go straight between the two views.
export default function Peer() {
  let bound = '';
  onViewMessage(({ data }) => {
    const id = (data as { port?: string }).port;
    if (!id || id === bound) return;
    bound = id;
    const port = portFromId(id);
    port.onmessage = (e) => {
      void port.postMessage({ pong: e.data });
    };
  });
  return <main id="peer">peer</main>;
}
