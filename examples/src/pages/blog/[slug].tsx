import { getCollection } from 'krate/content';

interface PostProps {
  params?: { slug: string };
}

export function generateStaticParams() {
  return getCollection('blog').map((p) => ({ slug: p.slug }));
}

export const dynamicParams = false;

export default function BlogPost(props: PostProps) {
  const slug = props.params?.slug;
  const post = getCollection('blog').find((p) => p.slug === slug);

  if (!post) {
    return <div class="blog-post"><h1>Not found</h1></div>;
  }

  return (
    <article class="blog-post">
      <Head>
        <title>{post.data.title}</title>
        <meta name="description" content={post.data.description} />
      </Head>
      <h1>{post.data.title}</h1>
      <div class="body" dangerouslySetInnerHTML={{ __html: post.html }}></div>
    </article>
  );
}
