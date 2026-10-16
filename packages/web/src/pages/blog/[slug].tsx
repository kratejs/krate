import { getCollection } from "krate/content";

interface PostProps {
  params?: { slug: string };
}

export function generateStaticParams() {
  return getCollection("blog")
    .filter((p) => !p.data.draft)
    .map((p) => ({ slug: p.slug }));
}

export const dynamicParams = false;

export default function BlogPost(props: PostProps) {
  const slug = props.params?.slug;
  const post = getCollection("blog").find((p) => p.slug === slug);

  if (!post) {
    return (
      <div class="not-found">
        <h1>Post not found</h1>
        <a class="btn btn-primary" href="/blog/">
          Back to the blog
        </a>
      </div>
    );
  }

  return (
    <article class="section prose">
      <Head>
        <title>{post.data.title}</title>
        <meta name="description" content={post.data.description} />
      </Head>
      <p>
        <a href="/blog/">&#8592; All posts</a>
      </p>
      <h1 class="section-title" style="text-align:left">
        {post.data.title}
      </h1>
      <p class="section-lede" style="text-align:left">
        {post.data.date}
      </p>
      <div dangerouslySetInnerHTML={{ __html: post.html }}></div>
    </article>
  );
}
