export const config = { streaming: true };
async function Slow() {
  await new Promise((r) => setTimeout(r, 200));
  return (
    <p id="slow">
      <Head>
        <meta name="description" content="from a streamed region" />
      </Head>
      slow-done
    </p>
  );
}
export default function HeadStream() {
  return (
    <main>
      <Head>
        <meta name="keywords" content="baked, static" />
      </Head>
      <h1 id="page">head stream</h1>
      <Suspense fallback={<p id="fb">loading-fallback</p>}>
        <Slow />
      </Suspense>
    </main>
  );
}
