import { notFound } from "@krate/runtime/server";

const posts: Record<string, { title: string; summary: string }> = {
  hello: { title: "Hello", summary: "first post" },
};

export async function generateMetadata({ params, searchParams }: any, parent: Promise<any>) {
  const { slug } = await params;
  const sp = await searchParams;
  if (slug === "gone") notFound();
  if (slug === "boom") throw new Error("lookup failed: secret detail");
  const post = await Promise.resolve(posts[slug]);
  const above = await parent;
  return {
    title: post.title,
    description: post.summary,
    alternates: { canonical: "https://example.com/meta/" + slug },
    openGraph: { title: post.title, images: ["/og/" + slug + ".png"], description: String(above.description) },
    ...(sp.draft ? { robots: { index: false, follow: false } } : {}),
  };
}

export default async function Post({ params }: any) {
  const { slug } = await params;
  return <h1 id="meta-page">post {slug}</h1>;
}
