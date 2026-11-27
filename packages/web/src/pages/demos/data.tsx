import Demo from "../../demos/data/demo";
import { DemoHead, DemoNotes, DemoSource, DemoPager } from "../../components/demo-page";

export default function Page() {
  return (
    <article class="demo-page">
      <DemoHead slug="data" />
      <div class="demo-body">
        <section class="demo-stage" aria-label="Live demo"><Demo /></section>
        <DemoNotes slug="data" />
      </div>
      <DemoSource slug="data" />
      <DemoPager slug="data" />
    </article>
  );
}
