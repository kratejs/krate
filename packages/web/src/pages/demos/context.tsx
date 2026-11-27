import Demo from "../../demos/context/demo";
import { DemoHead, DemoNotes, DemoSource, DemoPager } from "../../components/demo-page";

export default function Page() {
  return (
    <article class="demo-page">
      <DemoHead slug="context" />
      <div class="demo-body">
        <section class="demo-stage" aria-label="Live demo"><Demo /></section>
        <DemoNotes slug="context" />
      </div>
      <DemoSource slug="context" />
      <DemoPager slug="context" />
    </article>
  );
}
