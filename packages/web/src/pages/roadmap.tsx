const ITEMS = [
  { title: "Go-native compiler", status: "shipped", detail: "Custom lexer/parser/bundler — no bundler subprocess." },
  { title: "Signals + hydration", status: "shipped", detail: "Fine-grained reactivity with a tiny client bundle." },
  { title: "SSR / ISR / streaming", status: "shipped", detail: "Static-first shells with per-region request-time rendering." },
  { title: "Agent-native MCP", status: "shipped", detail: "Structured tools and resources for AI agents." },
  { title: "Unified site shell", status: "in-progress", detail: "One header/footer/theme across marketing and docs." },
  { title: "i18n + versioning", status: "in-progress", detail: "Path-prefix locales and multi-version docs." },
  { title: "Per-page OG images", status: "planned", detail: "Build-time social cards." },
  { title: "Runtime HMR", status: "planned", detail: "Component-boundary hot updates in dev." },
];

export default function Roadmap() {
  return (
    <article class="section">
      <Head>
        <title>Roadmap — Krate</title>
        <meta name="description" content="What's shipped and what's next for Krate." />
      </Head>
      <h1 class="section-title">Roadmap</h1>
      <p class="section-lede">Where Krate is and where it's going.</p>
      <ul class="taxonomy-list">
        {ITEMS.map((item) => (
          <li>
            <strong>{item.title}</strong>
            <span class="site-badge">{item.status}</span>
            <span> — {item.detail}</span>
          </li>
        ))}
      </ul>
    </article>
  );
}
