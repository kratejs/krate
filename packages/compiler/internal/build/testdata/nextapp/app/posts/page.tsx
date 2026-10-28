import Link from "next/link";
import LikeButton from "../components/LikeButton";
import AddPost from "../components/AddPost";

type Post = { slug: string; title: string };

async function getPosts(): Promise<Post[]> {
  const res = await fetch(`${process.env.DATA_URL}/api/posts`, { next: { tags: ["posts"] } });
  return res.json();
}

export default async function Posts() {
  const posts = await getPosts();
  return (
    <main id="posts">
      <h1>Posts</h1>
      <ul>
        {posts.map((p) => (
          <li key={p.slug}>
            <Link href={"/posts/" + p.slug}>{p.title}</Link> <LikeButton slug={p.slug} initial={0} />
          </li>
        ))}
      </ul>
      <AddPost />
    </main>
  );
}
