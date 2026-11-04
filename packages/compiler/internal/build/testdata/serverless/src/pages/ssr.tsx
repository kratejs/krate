export const config = { ssr: true };
export default async function Ssr({ searchParams }: any) {
  const q = await searchParams;
  return (
    <main>
      <h1 id="page">ssr</h1>
      <p id="q">{q.name ?? "anon"}</p>
    </main>
  );
}
