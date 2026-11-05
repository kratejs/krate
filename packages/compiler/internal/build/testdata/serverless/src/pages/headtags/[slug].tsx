export const config = { ssr: true };
export default async function HeadPost({ params }: any) {
  const { slug } = await params;
  return (
    <main>
      <Head>
        <title>Post {slug} | Krate</title>
        <meta name="description" content={`About <"${slug}"> & more`} />
        <meta property="og:title" content={"OG " + slug} />
        <meta property="og:image" content="https://example.com/a.png" />
        <meta property="og:image" content="https://example.com/b.png" />
        <link rel="canonical" href={"https://example.com/headtags/" + slug} />
        <script type="application/ld+json">{JSON.stringify({ "@type": "Article", name: slug, note: "</script>" })}</script>
      </Head>
      <h1 id="page">head {slug}</h1>
    </main>
  );
}
