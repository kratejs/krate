// JSX held in constants and passed as props: rendered to the page at build
// time and still correct after hydration. The browser conformance suite
// drives this page (internal/build/testdata/browser_conformance.mjs).
import { createSignal } from '@krate/runtime';

const notice = <p data-testid="module-notice">Module notice</p>;

function Panel(props: { intro: any }) {
  const [n, setN] = createSignal(0);
  return (
    <section data-testid="panel">
      <button data-testid="panel-inc" onClick={() => setN(n() + 1)}>{n()}</button>
      <div data-testid="panel-intro">{props.intro}</div>
    </section>
  );
}

export default function JsxValues() {
  const heading = 'Terms';
  const sections = [
    { id: 'a', title: 'First', body: (<p>Body of the first section.</p>) },
    { id: 'b', title: 'Second', body: (<><p>Body of the second section.</p></>) },
  ];
  return (
    <main>
      <div data-testid="local-list">
        {sections.map(s => <section key={s.id} id={s.id}><h2>{s.title}</h2>{s.body}</section>)}
      </div>
      {notice}
      <Panel intro={<em data-testid="prop-intro">{heading} intro</em>} />
    </main>
  );
}
