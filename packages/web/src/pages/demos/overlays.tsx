import Demo from "../../demos/overlays/demo";
import { DemoHead, DemoNotes, DemoSource, DemoPager } from "../../components/demo-page";

export default function Page() {
  return (
    <article class="demo-page">
      <DemoHead slug="overlays" />
      <div class="demo-body">
        <section class="demo-stage" aria-label="Live demo"><Demo /></section>
        <DemoNotes slug="overlays" />
      </div>
      <DemoSource slug="overlays" />
      <DemoPager slug="overlays" />
    </article>
  );
}
