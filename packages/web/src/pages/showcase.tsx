const PROJECTS = [
  {
    name: "Krate docs",
    url: "https://krate.js.org/docs/",
    detail: "This documentation site — built with Krate itself (docs plugin, WASM search, i18n).",
  },
  {
    name: "Krate examples",
    url: "https://github.com/kratejs/krate/tree/main/examples",
    detail: "A feature demo covering routing, SSR/ISR/streaming, API routes, workers and more.",
  },
];

export default function Showcase() {
  return (
    <article class="section">
      <Head>
        <title>Showcase — Krate</title>
        <meta name="description" content="Projects built with Krate." />
      </Head>
      <h1 class="section-title">Showcase</h1>
      <p class="section-lede">Sites and apps built with Krate.</p>
      <div class="features">
        {PROJECTS.map((p) => (
          <a class="feature-card" href={p.url}>
            <h3>{p.name}</h3>
            <p>{p.detail}</p>
          </a>
        ))}
      </div>
    </article>
  );
}
