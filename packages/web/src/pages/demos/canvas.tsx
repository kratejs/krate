import Demo from "../../demos/canvas/demo";
import { DemoHead, DemoNotes, DemoSource, DemoPager } from "../../components/demo-page";

export default function Page() {
  return (
    <article class="demo-page">
      <DemoHead slug="canvas" />
      <div class="demo-body">
        <section class="demo-stage" aria-label="Live demo"><Demo /></section>
        <DemoNotes slug="canvas" />
      </div>
      <DemoSource slug="canvas" />
      <DemoPager slug="canvas" />
    </article>
  );
}
