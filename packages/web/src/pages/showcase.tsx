const PROJECTS = [
  {
    name: "Live demos",
    url: "/demos/",
    detail: "Eleven interactive demos running on this static site: signals, zero-JS CSS signals, view transitions, canvas, forms and more, each with its source.",
  },
  {
    name: "Playground",
    url: "/playground/",
    detail: "The Krate compiler running in your browser as WebAssembly. Edit a project and see the HTML, JS and CSS it builds.",
  },
  {
    name: "Krate docs",
    url: "https://krate.js.org/docs/",
    detail: "This documentation site - built with Krate itself (docs plugin, Pagefind search, versioned pages).",
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
        <title>Showcase - Krate</title>
        <meta name="description" content="Projects built with Krate." />
      </Head>
      <h1 class="section-title">Showcase</h1>
      <p class="section-lede">Sites and apps built with Krate.</p>
      <h2 class="sr-only">Projects</h2>
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
