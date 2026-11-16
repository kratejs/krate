import { LazyViewer } from "../components/lazy-viewer";

export default async function Page() {
  const title = await Promise.resolve("lazy viewer");
  return (
    <main>
      <h1>{title}</h1>
      <LazyViewer url="/triangle.glb" />
    </main>
  );
}
