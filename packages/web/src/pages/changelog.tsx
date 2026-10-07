import { Code } from "@krate/components";

export default function Changelog() {
  return (
    <article class="section prose">
      <Head>
        <title>Changelog — Krate</title>
        <meta name="description" content="Krate release notes and changelog." />
      </Head>
      <h1 class="section-title" style="text-align:left">Changelog</h1>
      <p class="section-lede" style="text-align:left">
        Highlights of recent Krate releases. See the full history on
        <a href="https://github.com/kratejs/krate/releases"> GitHub Releases</a>.
      </p>

      <h2>Unreleased</h2>
      <ul>
        <li>Unified marketing/docs site shell (<code>HomeLayout</code>, shared header/footer, theme toggle).</li>
        <li>Headless docs search API (<code>window.__krateSearch</code>) + theme-owned <code>DocsSearch</code>.</li>
        <li>i18n path-prefix locales and versioned docs (switchers, hreflang, old-version banner).</li>
        <li>Mermaid diagrams, feeds (RSS/Atom/JSON), JSON-LD, per-page last-updated and taxonomies.</li>
        <li>Reliable SPA navigation (deterministic shell swap, race-safe fetches, head sync).</li>
      </ul>

      <h2>Getting a version</h2>
      <Code lang="bash">{`npm install -g @krate/core@latest`}</Code>
    </article>
  );
}
