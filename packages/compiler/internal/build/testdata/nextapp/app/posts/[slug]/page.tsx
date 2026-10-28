import { notFound } from "next/navigation";

export default async function Post({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const res = await fetch(`${process.env.DATA_URL}/api/posts/${slug}`, { next: { tags: ["posts"] } });
  if (res.status === 404) notFound();
  const post = await res.json();
  return <article id="post"><h1>{post.title}</h1></article>;
}
