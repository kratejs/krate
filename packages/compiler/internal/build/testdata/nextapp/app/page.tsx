import Link from "next/link";

export default function Feed() {
  return (
    <main id="feed">
      <h1>Feed</h1>
      <Link id="photo-1" href="/photo/1">Photo 1</Link>
    </main>
  );
}
