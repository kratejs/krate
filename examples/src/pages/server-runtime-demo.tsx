// @server
import ServerGreeting from '../components/ui/ServerGreeting.server';
import ServerTime from '../components/ui/ServerTime.server';
import RuntimeWidget from '../components/ui/RuntimeWidget.runtime';
import RuntimeCard from '../components/ui/RuntimeCard.runtime';
import { GoServerTime } from '../components/ui/GoServerTime.server.go';
import { GoRuntimeTime } from '../components/ui/GoRuntimeTime.runtime.go';

export default function ServerRuntimeDemo() {
  return (
    <div>
      <h1>Server &amp; Runtime Components Demo</h1>
      <ServerGreeting name="World" />
      <ServerTime />
      <RuntimeWidget label="Interactive Widget" />
      <RuntimeCard title="Runtime Card" />
      <h2>Go components</h2>
      <GoServerTime label="Go build time" />
      <GoRuntimeTime label="Go runtime time" />
    </div>
  );
}
