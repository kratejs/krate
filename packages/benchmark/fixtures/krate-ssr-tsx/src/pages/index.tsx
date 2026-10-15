// @server
import Widget from '../components/Widget.runtime';

// Streaming page: importing a runtime component makes the shell bake and the
// widget render per request (via the SSR sidecar).
export default function Page() {
  return (
    <main>
      <h1>SSR benchmark</h1>
      <Widget label="runtime-tsx" />
    </main>
  );
}
