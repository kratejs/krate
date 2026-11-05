export const config = { ssr: true };
async function Author() {
  await new Promise((r) => setTimeout(r, 5));
  return (
    <p id="author">
      <Head>
        <meta name="author" content="Ada" />
        <title>Written by Ada</title>
      </Head>
      by Ada
    </p>
  );
}
export default function HeadComponent() {
  return (
    <main>
      <h1 id="page">head component</h1>
      <Author />
    </main>
  );
}
