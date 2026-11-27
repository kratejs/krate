import sources from "../demos/sources.json";
import output from "../demos/compiled/output.json";
import benchmarks from "../data/benchmarks.json";
import { demos } from "../demos/registry";

// Static panes. They hold the data (sources, build output, benchmark numbers)
// so the components that own a CSS-signal scope or an island stay data-free;
// otherwise the compiler copies that data into the page's hydration script.

const counterSource = `export default function Counter() {
  const [count, setCount] = createSignal(0);
  return (
    <div>
      <span>{count()}</span>
      <button onClick={() => setCount(c => c + 1)}>+</button>
    </div>
  );
}`;

const kb = (n: number) => (n / 1024 >= 10 ? Math.round(n / 1024) : (n / 1024).toFixed(1)) + " KB";
const ms = (n: number) => (n < 1000 ? Math.round(n) + " ms" : (n / 1000).toFixed(1) + " s");
const rps = (n: number) => (n >= 1000 ? Math.round(n / 1000) + "k/s" : Math.round(n) + "/s");

export function Snippet(props: { name: string }) {
  const code = props.name === "counter.tsx" ? counterSource : sources.home.find((f) => f.name === props.name)!.code;
  return (
    <div class="krate-code-block">
      <div class="krate-code-header"><div class="krate-code-title">{props.name}</div></div>
      <SyntaxHighlight lang="tsx">{code}</SyntaxHighlight>
    </div>
  );
}

export function CompiledTsx() {
  return <div class="krate-code-block"><SyntaxHighlight lang="tsx">{sources.compiled[0].code}</SyntaxHighlight></div>;
}
export function CompiledHtml() {
  return <div class="krate-code-block"><SyntaxHighlight lang="html">{output.html}</SyntaxHighlight></div>;
}
export function CompiledJs() {
  return <div class="krate-code-block"><SyntaxHighlight lang="javascript">{output.js}</SyntaxHighlight></div>;
}
export function CompiledFacts(props: { kind: string }) {
  return props.kind === "html"
    ? <>{output.htmlBytes} B</>
    : props.kind === "js"
      ? <>{output.jsGzipBytes} B gzip</>
      : <>{output.jsBytes} byte hydration script and a shared signal runtime of {kb(output.runtimeGzipBytes)} gzipped</>;
}

export function BenchTable(props: { metric: string }) {
  const m = props.metric;
  const rows =
    m === "build"
      ? [...benchmarks.build].sort((a, b) => a.medianMs - b.medianMs).map((r) => ({ id: r.id, name: r.name, v: r.medianMs, text: ms(r.medianMs) }))
      : m === "js"
        ? [...benchmarks.build].sort((a, b) => a.clientJsGzipBytes! - b.clientJsGzipBytes!).map((r) => ({ id: r.id, name: r.name, v: r.clientJsGzipBytes!, text: kb(r.clientJsGzipBytes!) }))
        : benchmarks.api
            .filter((r) => ["krate-go", "go-nethttp", "node-http", "krate-ts-node", "nextjs", "krate-ts"].includes(r.id))
            .sort((a, b) => b.rps - a.rps)
            .map((r) => ({ id: r.id, name: r.name, v: r.rps, text: rps(r.rps) }));
  const max = Math.max(...rows.map((r) => r.v));
  const caption =
    m === "build"
      ? "Cold build of the same counter page. Shorter is better."
      : m === "js"
        ? "Client JavaScript shipped for the counter page, gzipped. Smaller is better."
        : "A JSON API route under 32 connections. Higher is better.";
  return (
    <table class="bc-table">
      <caption>{caption}</caption>
      <thead><tr><th scope="col">{m === "api" ? "Server" : "Framework"}</th><th scope="col">Bar</th><th scope="col">Result</th></tr></thead>
      <tbody>
        {rows.map((r) => (
          <tr class={r.id === "krate" || r.id === "krate-go" ? "bc-krate" : ""}>
            <th scope="row">{r.name}</th>
            <td><span class="bc-bar" style={"width: " + Math.max(1.5, (r.v / max) * 100).toFixed(1) + "%"}></span></td>
            <td class="bc-val">{r.text}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

export function BenchNote() {
  return <p class="bc-note">Median of {benchmarks.buildMeta.runs} runs on a {benchmarks.meta.cpus}-core machine, from the published benchmark run. <a href="/docs/guides/benchmarks/">Methodology</a>.</p>;
}

export function DemoTeasers() {
  const list = ["reactivity", "css-signals", "tasks", "transitions", "canvas", "overlays"].map((slug) => demos.find((d) => d.slug === slug)!);
  return (
    <ul class="demos-grid home-demos">
      {list.map((d) => (
        <li>
          <a class="demo-card" href={"/demos/" + d.slug + "/"}>
            <span class="demo-card-top">
              <span class="demo-card-icon"><Icon name={d.icon} width="22" height="22" /></span>
              <span class={d.js === "zero" ? "demo-chip demo-chip-zero" : "demo-chip demo-chip-tag"}>{d.js === "zero" ? "0 JS" : "Island"}</span>
            </span>
            <h3>{d.title}</h3>
            <p>{d.blurb}</p>
          </a>
        </li>
      ))}
    </ul>
  );
}
