import { Code } from "@krate/components/code";
import { HeroTabs, Pipeline, BenchChart } from "../home/sections";
import { DemoTeasers } from "../home/panes";
import "../styles/demos.css";
import "../styles/home.css";
import benchmarks from "../data/benchmarks.json";

// Scores come from the published benchmark run (packages/benchmark, `publish`).
const krateBuild = benchmarks.build.find((r) => r.id === "krate")!;
const nextBuild = benchmarks.build.find((r) => r.id === "nextjs")!;
const goApi = benchmarks.api.find((r) => r.id === "krate-go")!;
const kb = (n: number) => `${Math.round(n / 1024)} KB`;
const thousands = (n: number) => `${Math.round(n / 1000)}k`;
const buildTime = (n: number) => (n < 1000 ? `${Math.round(n)} ms` : `${(n / 1000).toFixed(1)} s`);

export default function Home() {
  return (
    <>
      <Head>
        <title>Krate - Go-native web framework</title>
        <meta name="description" content="Krate compiles TSX pages to static HTML with a small signal runtime. Write server components and API routes in Go - one binary, no Node required to build." />
      </Head>

      <section class="hero">
        <h1 class="hero-title">Web applications with the power of a compiler.</h1>
        <p class="hero-subtitle">
          Krate compiles TSX/JSX into static HTML at build time and ships a small
          signal-based hydration bundle (about {kb(krateBuild.clientJsGzipBytes!)} gzipped for a counter page). Fine-grained reactivity, file-based
          routing, SSR/ISR/streaming, a CSS pipeline, and a plugin system - all
          driven by a custom Go compiler. Drop into Go for server components and
          API routes whenever you want.
        </p>
        <div class="hero-actions">
          <a class="btn btn-primary" href="/docs/">Read the docs</a>
          <a class="btn btn-secondary" href="/docs/getting-started/">Getting started</a>
          <a class="btn btn-secondary" href="/demos/">Live demos</a>
          <a class="btn btn-secondary" href="/playground/">Try it in the playground</a>
          <a class="btn btn-secondary" href="/blog/">Blog</a>
        </div>
        <HeroTabs />
      </section>

      <section class="section">
        <h2 class="section-title">From TSX to a few hundred bytes.</h2>
        <p class="section-lede">
          Step through what the compiler does to a counter. This is real output, built with the
          same compiler that runs in the <a href="/playground/">playground</a>.
        </p>
        <Pipeline />
      </section>

      <section class="features bento">
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
        <h2 class="section-title">See it run.</h2>
        <p class="section-lede">
          Eleven live demos, each with its source and a one-click link into the playground:
          reactivity you can watch, widgets that work without JavaScript, view transitions, a canvas and more.
          <a href="/demos/">Browse all demos</a>.
        </p>
        <DemoTeasers />
      </section>

      <section class="section">
        <h2 class="section-title">How Krate compares</h2>
        <p class="section-lede">
          A Go-native compiler with whole-program knowledge, a signal runtime,
          and a static-first default.
        </p>
        <div class="table-wrap" role="region" aria-label="Framework comparison" tabindex={0}>
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
          A cold build in {buildTime(krateBuild.medianMs)} against {buildTime(nextBuild.medianMs)} for
          Next.js on the same page, and Go API routes compiled into a native
          sidecar. <a href="/docs/guides/benchmarks/">See the benchmarks</a>.
        </p>
        <BenchChart />
        <div class="stats">
          <div class="stat"><div class="stat-value">{buildTime(krateBuild.medianMs)}</div><div class="stat-label">Cold build, counter page</div></div>
          <div class="stat"><div class="stat-value">{thousands(goApi.rps)}</div><div class="stat-label">req/s, Go API route</div></div>
          <div class="stat"><div class="stat-value">{kb(krateBuild.clientJsGzipBytes!)}</div><div class="stat-label">JS gzipped, counter page</div></div>
          <div class="stat"><div class="stat-value">0 kB</div><div class="stat-label">JS on static pages</div></div>
        </div>
      </section>

      <section class="cta-band">
        <div class="cta-copy">
          <h2>Build something great.</h2>
          <p>Install the CLI and scaffold a project in seconds.</p>
          <a class="btn btn-primary" href="/docs/getting-started/">Get started</a>
        </div>
        <Code lang="bash" showCopy>{`npm install -g @krate/core
mkdir my-app && cd my-app
krate init
krate dev`}</Code>
      </section>
    </>
  );
}
