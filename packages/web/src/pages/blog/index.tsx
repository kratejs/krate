const POSTS = [
  {
    slug: "why-krate",
    title: "Why a Go-native web framework",
    date: "2026-03-01",
    summary: "Whole-program compilation, signals, and zero Node at build time.",
  },
  {
    slug: "hydration-without-react",
    title: "Hydration without React",
    date: "2026-02-10",
    summary: "How Krate binds signals to the DOM with a tiny client runtime.",
  },
];

export default function Blog() {
  return (
    <article class="section">
      <Head>
        <title>Blog — Krate</title>
        <meta name="description" content="News, deep dives, and release notes from the Krate team." />
      </Head>
      <h1 class="section-title">Blog</h1>
      <p class="section-lede">News and deep dives from the Krate team.</p>
      <ul class="taxonomy-list">
        {POSTS.map((p) => (
          <li>
            <a href={"/blog/" + p.slug + "/"}>{p.title}</a>
            <span class="site-badge">{p.date}</span>
            <span> — {p.summary}</span>
          </li>
        ))}
      </ul>
    </article>
  );
}
