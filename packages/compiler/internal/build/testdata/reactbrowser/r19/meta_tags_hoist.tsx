import { useState } from "react";

const posts = [
  { slug: "signals", title: "Signals explained", summary: "How fine-grained updates work." },
  { slug: "islands", title: "Islands", summary: "Partial hydration in practice." },
];

function Post(props: { index: number }) {
  const p = posts[props.index];
  return (
    <article>
      <title>{p.title}</title>
      <meta name="description" content={p.summary} />
      <link rel="canonical" href={"https://example.com/blog/" + p.slug} />
      <h1 id="title">{p.title}</h1>
      <p>{p.summary}</p>
    </article>
  );
}

// React 19 hoists <meta> and <link> rendered in components into <head>.
export default function MetaTagsHoist() {
  const [index, setIndex] = useState(0);
  return (
    <div>
      <Post index={index} />
      <button id="next" onClick={() => setIndex((index + 1) % posts.length)}>
        next post
      </button>
    </div>
  );
}
