// The editor on a server-rendered page: the "use client" module runs as a
// Preact island, where @krate/konva resolves to its island build.
import Editor from "../components/editor";

export default async function Page() {
  const title = await Promise.resolve("konva island");
  return (
    <main>
      <h1>{title}</h1>
      <Editor />
    </main>
  );
}
