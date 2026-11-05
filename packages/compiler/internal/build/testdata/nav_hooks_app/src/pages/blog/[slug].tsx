import { usePathname, useParams } from "@krate/runtime";

export function generateStaticParams() {
  return [{ slug: "a" }, { slug: "b" }];
}

export default function Post() {
  const pathname = usePathname();
  const params = useParams();
  return (
    <main>
      <h1 id="post">post</h1>
      <p id="post-path">{pathname}</p>
      <p id="post-slug">{params.slug}</p>
    </main>
  );
}
