import { getCollection } from 'krate/content';

export default function BlogIndex() {
  const posts = getCollection('blog')
    .filter((p) => !p.data.draft)
    .sort((a, b) => a.data.order - b.data.order);

  return (
    <div class="blog-index">
      <h1>Blog</h1>
      <ul>
        {posts.map((post) => (
          <li>
            <a href={`/blog/${post.slug}`}>{post.data.title}</a>
          </li>
        ))}
      </ul>
    </div>
  );
}
