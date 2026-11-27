import Demo from "../../demos/reactivity/demo";
import { DemoHead, DemoNotes, DemoSource, DemoPager } from "../../components/demo-page";

export default function Page() {
  return (
    <article class="demo-page">
      <DemoHead slug="reactivity" />
      <div class="demo-body">
        <section class="demo-stage" aria-label="Live demo"><Demo /></section>
        <DemoNotes slug="reactivity" />
      </div>
      <DemoSource slug="reactivity" />
      <DemoPager slug="reactivity" />
    </article>
  );
}
