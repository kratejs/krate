import Demo from "../../demos/transitions/demo";
import { DemoHead, DemoNotes, DemoSource, DemoPager } from "../../components/demo-page";

export default function Page() {
  return (
    <article class="demo-page">
      <DemoHead slug="transitions" />
      <div class="demo-body">
        <section class="demo-stage" aria-label="Live demo"><Demo /></section>
        <DemoNotes slug="transitions" />
      </div>
      <DemoSource slug="transitions" />
      <DemoPager slug="transitions" />
    </article>
  );
}
