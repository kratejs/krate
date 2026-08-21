function About() {
  return (
    <>
      <Head>
        <title>About Krate</title>
        <meta name="description" content="Learn about Krate framework" />
      </Head>
      <div class="card">
        <h1>About Krate</h1>
        <p>Krate is a modern web framework that compiles JSX and TSX components into optimized static HTML with a tiny client-side runtime for hydration.</p>
      </div>
      <div class="card">
        <h2>How It Works</h2>
        <p>The Go compiler bundles your source files, parses them into an AST, and renders components to HTML at build time. Signals and event handlers are serialized into a minimal hydration script.</p>
      </div>
      <div class="card">
        <h2>Key Features</h2>
        <ul>
          <li><strong>Go Compiler</strong> - Blazing fast builds written in Go</li>
          <li><strong>File-based Routing</strong> - Pages in src/pages/ become routes</li>
          <li><strong>Layouts</strong> - Shared UI via _layout.tsx</li>
          <li><strong>Signals</strong> - Fine-grained reactivity with createSignal</li>
          <li><strong>Static Generation</strong> - Compile-time rendering for fast pages</li>
        </ul>
      </div>
      <div class="card">
        <h2>Architecture</h2>
        <p>The monorepo has two main packages:</p>
        <ul>
          <li><strong>compiler/</strong> - Go compiler (bundler, parser, renderer)</li>
          <li><strong>runtime/</strong> - Client-side runtime (signals, hydration, JSX runtime)</li>
        </ul>
      </div>
    </>
  );
}

export default About;
