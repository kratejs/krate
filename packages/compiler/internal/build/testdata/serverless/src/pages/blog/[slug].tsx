export function generateStaticParams() {
  return [{ slug: "first" }, { slug: "second" }];
}
export default function Post({ params }: any) {
  return (
    <main>
      <h1 id="page">post {params.slug}</h1>
      <p class="slug">{params.slug}</p>
    </main>
  );
}
