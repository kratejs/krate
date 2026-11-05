export const revalidate = 3600;

// Reads a data source with a tag: the page is cached (ISR) and the fetch is
// cached (data cache), both tagged "items".
export default async function Items() {
  const res = await fetch(process.env.DATA_URL + "/items", { next: { tags: ["items"], revalidate: 3600 } });
  const data = await res.json();
  return (
    <main>
      <h1 id="v">version {data.version}</h1>
    </main>
  );
}
