export const config = { isr: true, revalidate: 30 };

export async function generateMetadata() {
  await Promise.resolve();
  return {
    title: "From generateMetadata",
    description: "meta description",
    robots: { index: false, follow: false },
    openGraph: { title: "OG from meta", images: ["https://example.com/meta.png"] },
  };
}

export default async function MixHead() {
  await Promise.resolve();
  return (
    <main>
      <Head>
        <meta name="description" content="head description" />
        <meta name="robots" content="noindex, nofollow" />
        <meta name="author" content="Head" />
        <meta property="og:image" content="https://example.com/head.png" />
        <meta property="og:image" content="https://example.com/meta.png" />
      </Head>
      <h1 id="page">mixed head</h1>
    </main>
  );
}
