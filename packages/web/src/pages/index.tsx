import { Code } from "@krate/components";
import { CounterDemo } from "../components/counter-demo";

export default function Home() {
  return (
    <>
      <Head>
        <title>Krate - Go-native web framework</title>
        <meta name="description" content="Krate compiles TSX pages to static HTML with a tiny signal runtime. Write server components and API routes in Go - one binary, no Node required to build." />
      </Head>

      <section class="hero">
        <h1 class="hero-title">Web applications with the power of a compiler.</h1>
        <p class="hero-subtitle">
          Krate compiles TSX/JSX into static HTML at build time and ships a tiny
          signal-based hydration bundle. Fine-grained reactivity, file-based
          routing, SSR/ISR/streaming, a CSS pipeline, and a plugin system - all
          driven by a custom Go compiler. Drop into Go for server components and
          API routes whenever you want.
        </p>
        <div class="hero-actions">
          <a class="btn btn-primary" href="/docs/">Read the docs</a>
          <a class="btn btn-secondary" href="/docs/getting-started/">Getting started</a>
          <a class="btn btn-secondary" href="/blog/">Blog</a>
        </div>
        <div class="hero-demo">
          <div class="hero-demo-code"><Code lang="typescript" showCopy title="counter.tsx">{`export default function Counter() {
  const [count, setCount] = createSignal(0);
  return (
    <div>
      <span>{count()}</span>
      <button onClick={() => setCount(c => c + 1)}>+</button>
    </div>
  );
}`}</Code></div>
          <div class="hero-demo-live">
            <span class="hero-demo-live-label">Live preview</span>
            <CounterDemo />
            <span class="hero-demo-live-note">Signals update the DOM directly - no virtual DOM.</span>
          </div>
        </div>
      </section>

      <section class="features">
        <h2 class="sr-only">Features</h2>
        <a class="feature-card" href="/docs/core-concepts/reactivity/">
          <h3>Signals, not React</h3>
          <p>Fine-grained reactivity with <code>createSignal</code>, <code>createEffect</code> and <code>createMemo</code> - React syntax is transpiled automatically.</p>
        </a>
        <a class="feature-card" href="/docs/core-concepts/rendering/">
          <h3>SSG-first rendering</h3>
          <p>Every page pre-rendered to static HTML. Add SSR, ISR and Suspense-based streaming when you need it.</p>
        </a>
        <a class="feature-card" href="/docs/features/go-components/">
          <h3>Go components</h3>
          <p>Author server and runtime components in Go, importing any Go package. They compile into the same host binary as your API routes.</p>
        </a>
        <a class="feature-card" href="/docs/features/api-routes/">
          <h3>JS &amp; Go API routes</h3>
          <p>Write API routes in TypeScript or Go; both compile into the same <code>/api/*</code> namespace and one binary.</p>
        </a>
        <a class="feature-card" href="/docs/core-concepts/styling/">
          <h3>Full CSS pipeline</h3>
          <p>CSS Modules with FNV-32a scoping, Go-native Tailwind, minification and @import inlining.</p>
        </a>
        <a class="feature-card" href="/docs/features/plugins/">
          <h3>Plugin system</h3>
          <p>Native plugins compiled with Go, or plugins written in JavaScript, executed inside an embedded QuickJS runtime.</p>
        </a>
        <a class="feature-card" href="/docs/features/search/">
          <h3>WASM-powered search</h3>
          <p>Docs search with a build-time index served locally - zero server round-trips.</p>
        </a>
        <a class="feature-card" href="/docs/features/mcp/">
          <h3>Agent-native (MCP)</h3>
          <p>Drive the compiler from an AI agent over the Model Context Protocol with <code>krate mcp</code>.</p>
        </a>
      </section>

      <section class="section">
        <h2 class="section-title">How Krate compares</h2>
        <p class="section-lede">
          A Go-native compiler with whole-program knowledge, a signal runtime,
          and a static-first default.
        </p>
        <div class="table-wrap">
          <table class="compare">
            <thead>
              <tr><th>Capability</th><th>Krate</th><th>Astro</th><th>Next.js</th></tr>
            </thead>
            <tbody>
              <tr><td>Compiler language</td><td>Go (custom lexer/parser)</td><td>Vite/esbuild</td><td>SWC/Turbopack</td></tr>
              <tr><td>Client runtime by default</td><td class="yes">Zero JS</td><td>Per-island</td><td>React runtime</td></tr>
              <tr><td>Interactivity with no JS</td><td class="yes">CSS signals</td><td class="no">No</td><td class="no">No</td></tr>
              <tr><td>Node required to build</td><td class="yes">No</td><td>Yes</td><td>Yes</td></tr>
              <tr><td>Go components &amp; API routes</td><td class="yes">One binary</td><td class="no">No</td><td class="no">No</td></tr>
              <tr><td>SSR / ISR / streaming</td><td class="yes">Yes (sidecar)</td><td>Yes</td><td>Yes</td></tr>
              <tr><td>Agent-native (MCP)</td><td class="yes">Yes</td><td class="no">No</td><td class="no">No</td></tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="section">
        <h2 class="section-title">Built for speed</h2>
        <p class="section-lede">
          A Go API route saturates a core; a runtime Go component renders on a
          compiled host with no Node in the path. <a href="https://github.com/kratejs/krate/tree/main/packages/benchmark">See the benchmarks</a>.
        </p>
        <div class="stats">
          <div class="stat"><div class="stat-value">46k</div><div class="stat-label">req/s, Go API route</div></div>
          <div class="stat"><div class="stat-value">0 kB</div><div class="stat-label">JS on static pages</div></div>
          <div class="stat"><div class="stat-value">1</div><div class="stat-label">Binary, no Node</div></div>
          <div class="stat"><div class="stat-value">ES2020</div><div class="stat-label">Output baseline</div></div>
        </div>
      </section>

      <section class="cta-band">
        <h2>Build something great.</h2>
        <p>Install the CLI and scaffold a project in seconds.</p>
        <Code lang="bash" showCopy>{`npm install -g @krate/core
mkdir my-app && cd my-app
krate init
krate dev`}</Code>
        <a class="btn btn-primary" href="/docs/getting-started/">Get started</a>
      </section>
    </>
  );
}
