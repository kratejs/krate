import Viewer from "../components/viewer";

export default async function Page() {
  const title = await Promise.resolve("broken model");
  return (
    <main>
      <h1>{title}</h1>
      <Viewer url="/missing.glb" />
    </main>
  );
}
