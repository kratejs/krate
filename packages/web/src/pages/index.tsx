export default function Home() {
  return (
    <>
      <Head>
        <title>Krate — Go-native static site generator</title>
        <meta name="description" content="Krate compiles TSX/JSX pages into static HTML at build time with a tiny signal-based hydration bundle. No React, no bundler subprocess, no Node.js required." />
      </Head>

      <section class="hero">
        <div class="hero-badge">Signal-based · SSG-first · Go-native</div>
        <h1 class="hero-title">Static sites with the power of a compiler.</h1>
        <p class="hero-subtitle">
          Krate compiles TSX/JSX into static HTML at build time and ships a tiny
          signal-based hydration bundle. Fine-grained reactivity, file-based
          routing, SSR/ISR/streaming, a CSS pipeline, and a plugin system — all
          driven by a custom Go compiler.
        </p>
        <div class="hero-actions">
          <a class="btn btn-primary" href="/docs/">Read the docs</a>
          <a class="btn btn-secondary" href="/docs/getting-started/">Getting started</a>
        </div>
        <div class="hero-code"><SyntaxHighlight lang="typescript">{`export default function Counter() {
  const [count, setCount] = createSignal(0);
  return (
    <div>
      <span>{count()}</span>
      <button onClick={() => setCount(c => c + 1)}>+</button>
    </div>
  );
}`}</SyntaxHighlight></div>
      </section>

      <section class="features">
        <a class="feature-card" href="/docs/core-concepts/reactivity/">
          <h3>Signals, not React</h3>
          <p>Fine-grained reactivity with <code>createSignal</code>, <code>createEffect</code> and <code>createMemo</code> — compatible with React via <code>emitReact</code>.</p>
        </a>
        <a class="feature-card" href="/docs/core-concepts/rendering/">
          <h3>SSG-first rendering</h3>
          <p>Every page pre-rendered to static HTML. Add SSR, ISR and Suspense-based streaming when you need it.</p>
        </a>
        <a class="feature-card" href="/docs/core-concepts/styling/">
          <h3>Full CSS pipeline</h3>
          <p>CSS Modules with FNV-32a scoping, Go-native Tailwind, minification and @import inlining.</p>
        </a>
        <a class="feature-card" href="/docs/features/plugins/">
          <h3>Plugin system</h3>
          <p>Go hooks plus community plugins written in JavaScript, executed inside an embedded QuickJS runtime.</p>
        </a>
        <a class="feature-card" href="/docs/features/search/">
          <h3>WASM-powered search</h3>
          <p>Docs search with a docfind index embedded into a WASM module at build time — zero server round-trips.</p>
        </a>
        <a class="feature-card" href="/docs/features/api-routes/">
          <h3>JS & Go API routes</h3>
          <p>Write API routes in TypeScript or Go; both compile into the same <code>/api/*</code> namespace.</p>
        </a>
      </section>

      <section class="cta-band">
        <h2>Build something great.</h2>
        <p>Install the CLI and scaffold a project in seconds.</p>
        <SyntaxHighlight lang="bash">{`npm install -g @krate/core
mkdir my-app && cd my-app
krate init
krate dev`}</SyntaxHighlight>
        <a class="btn btn-primary" href="/docs/getting-started/">Get started</a>
      </section>
    </>
  );
}
