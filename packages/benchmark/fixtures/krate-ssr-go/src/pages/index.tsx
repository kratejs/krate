// @server
import { Widget } from '../components/Widget.runtime.go';

// Streaming page: the Go runtime component renders per request on the Go host.
export default function Page() {
  return (
    <main>
      <h1>SSR benchmark</h1>
      <Widget label="runtime-go" />
    </main>
  );
}
