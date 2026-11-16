import Viewer from "../components/viewer";

export default async function Page() {
  const title = await Promise.resolve("island three");
  return (
    <main>
      <h1>{title}</h1>
      <Viewer url="/triangle.glb" />
    </main>
  );
}
