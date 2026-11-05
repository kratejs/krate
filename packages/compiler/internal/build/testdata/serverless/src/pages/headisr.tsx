export const config = { isr: true, revalidate: 30 };
export default async function HeadIsr() {
  return (
    <main>
      <Head>
        <title>ISR head title</title>
        <meta name="robots" content="noindex" />
        <meta property="og:type" content="website" />
      </Head>
      <h1 id="page">isr head</h1>
    </main>
  );
}
