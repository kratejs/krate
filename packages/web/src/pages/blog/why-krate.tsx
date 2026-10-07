export default function WhyKrate() {
  return (
    <article class="section prose">
      <Head>
        <title>Why a Go-native web framework — Krate</title>
        <meta name="description" content="Whole-program compilation, signals, and zero Node at build time." />
      </Head>
      <p>
        <a href="/blog/">← All posts</a>
      </p>
      <h1 class="section-title" style="text-align:left">Why a Go-native web framework</h1>
      <p class="section-lede" style="text-align:left">2026-03-01</p>

      <p>
        Most JS meta-frameworks compile your app with a JavaScript toolchain running on
        Node. Krate starts from a different premise: your app is <em>data</em> that a Go
        binary can lex, parse, analyze, and render into static HTML.
      </p>

      <h2>Whole-program by construction</h2>
      <p>
        Because the compiler sees the whole program, it can fold data fetching at build
        time, inline components, and emit only the hydration code a page actually needs.
        There is no separate bundler to configure and no Node process to supervise.
      </p>

      <h2>Signals over virtual DOM</h2>
      <p>
        Interactivity is expressed as signals. The runtime binds them directly to DOM
        nodes, so updates are surgical and the client payload stays small.
      </p>
    </article>
  );
}
