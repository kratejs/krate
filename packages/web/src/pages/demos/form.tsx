import Demo from "../../demos/form/demo";
import { DemoHead, DemoNotes, DemoSource, DemoPager } from "../../components/demo-page";

export default function Page() {
  return (
    <article class="demo-page">
      <DemoHead slug="form" />
      <div class="demo-body">
        <section class="demo-stage" aria-label="Live demo"><Demo /></section>
        <DemoNotes slug="form" />
      </div>
      <DemoSource slug="form" />
      <DemoPager slug="form" />
    </article>
  );
}
