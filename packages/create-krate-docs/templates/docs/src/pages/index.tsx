export default function Home() {
  return (
    <>
      <Head>
        <title>__PROJECT_DISPLAY_NAME__ — Documentation</title>
        <meta
          name="description"
          content="Documentation site built with Krate."
        />
      </Head>

      <section class="hero">
        <div class="hero-badge">Powered by Krate</div>
        <h1 class="hero-title">__PROJECT_DISPLAY_NAME__</h1>
        <p class="hero-subtitle">
          This is your documentation home. Content lives in{" "}
          <code>content/docs/</code> as Markdown — add files there and they
          appear in the sidebar automatically, with search, a table of contents,
          and prev/next navigation.
        </p>
        <div class="hero-actions">
          <a class="btn btn-primary" href="/docs/">Read the docs</a>
          <a class="btn btn-secondary" href="/docs/getting-started/">
            Getting started
          </a>
        </div>
      </section>

      <section class="features">
        <a class="feature-card" href="/docs/">
          <h3>Markdown-first</h3>
          <p>
            Write pages in Markdown or MDX under <code>content/docs/</code>.
            Frontmatter controls titles and sidebar ordering.
          </p>
        </a>
        <a class="feature-card" href="/docs/getting-started/">
          <h3>Full docs UX</h3>
          <p>
            Sidebar, table of contents, breadcrumbs, prev/next links, and
            light/dark theme out of the box.
          </p>
        </a>
        <a class="feature-card" href="/docs/">
          <h3>WASM search</h3>
          <p>
            Full-text search powered by docfind, with the index embedded at
            build time — zero server round-trips.
          </p>
        </a>
      </section>

      <section class="cta-band">
        <h2>Start writing.</h2>
        <p>Add your first page under <code>content/docs/</code>.</p>
        <a class="btn btn-primary" href="/docs/getting-started/">Get started</a>
      </section>
    </>
  );
}
