// @server
import ServerGreeting from '../components/ui/ServerGreeting.server';
import ServerTime from '../components/ui/ServerTime.server';
import RuntimeWidget from '../components/ui/RuntimeWidget.runtime';
import RuntimeCard from '../components/ui/RuntimeCard.runtime';

export default function ServerRuntimeDemo() {
  return (
    <div>
      <h1>Server &amp; Runtime Components Demo</h1>
      <ServerGreeting name="World" />
      <ServerTime />
      <RuntimeWidget label="Interactive Widget" />
      <RuntimeCard title="Runtime Card" />
    </div>
  );
}
