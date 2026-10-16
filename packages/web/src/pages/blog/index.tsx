import { getCollection } from "krate/content";

export default function Blog() {
  const posts = getCollection("blog")
    .filter((p) => !p.data.draft)
    .sort((a, b) => (a.data.date < b.data.date ? 1 : -1));

  return (
    <article class="section">
      <Head>
        <title>Blog - Krate</title>
        <meta name="description" content="News, deep dives, and release notes from the Krate team." />
      </Head>
      <h1 class="section-title">Blog</h1>
      <p class="section-lede">News and deep dives from the Krate team.</p>
      <ul class="taxonomy-list">
        {posts.map((p) => (
          <li>
            <a href={`/blog/${p.slug}/`}>{p.data.title}</a>
            <span class="site-badge">{p.data.date}</span>
            <span> - {p.data.description}</span>
          </li>
        ))}
      </ul>
    </article>
  );
}
