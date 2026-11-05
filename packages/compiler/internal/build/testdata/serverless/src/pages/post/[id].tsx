import { notFound, redirect, cookies, forbidden, unauthorized } from "@krate/runtime/server";
export default async function Post({ params }: any) {
  const { id } = await params;
  if (id === "missing") notFound();
  if (id === "old") redirect("/post/new");
  if (id === "secret") forbidden();
  if (id === "anon") unauthorized();
  if (id === "boom") throw new Error("exploded on " + id);
  const jar = await cookies();
  return (
    <article>
      <h1 id="page">post {id}</h1>
      <p id="sid">{jar.get("sid")?.value ?? "none"}</p>
    </article>
  );
}
