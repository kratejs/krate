import { SaveForm } from "../components/form";
import { save, saveTagged } from "../actions/save";

export default async function Home() {
  await Promise.resolve();
  return (
    <main>
      <SaveForm id="plain" action={save} />
      <SaveForm id="bound" action={saveTagged.bind(null, "tag")} />
    </main>
  );
}
