import { DirectForm, SaveForm } from "../components/form";
import { ping, save, saveTagged } from "../actions/save";

export default async function Home() {
  await Promise.resolve();
  return (
    <main>
      <SaveForm id="plain" action={save} />
      <SaveForm id="bound" action={saveTagged.bind(null, "s3cret-tag")} />
      <DirectForm id="direct" action={ping} />
    </main>
  );
}
