export const config = { streaming: true };
async function Slow() {
  await new Promise((r) => setTimeout(r, 400));
  return <p id="slow">slow-done</p>;
}
export default function Stream() {
  return (
    <main>
      <h1 id="page">stream</h1>
      <Suspense fallback={<p id="fb">loading-fallback</p>}>
        <Slow />
      </Suspense>
    </main>
  );
}
