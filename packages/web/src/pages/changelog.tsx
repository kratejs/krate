export default function Changelog() {
  return (
    <article class="section">
      <Head>
        <title>Changelog — Krate</title>
        <meta name="description" content="Krate release notes and changelog." />
      </Head>
      <h1 class="section-title">Changelog</h1>
      <p class="section-lede">
        Every release and its full notes are published on GitHub Releases.
        Breaking changes will be documented here as they occur.
      </p>
      <div class="hero-actions">
        <a class="btn btn-primary" href="https://github.com/kratejs/krate/releases">
          GitHub Releases
        </a>
        <a class="btn btn-secondary" href="https://github.com/kratejs/krate/releases.atom">
          Release feed (Atom)
        </a>
      </div>
    </article>
  );
}
