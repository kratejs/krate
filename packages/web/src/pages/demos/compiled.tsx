import Demo from "../../demos/compiled/demo";
import { DemoHead, DemoNotes, DemoSource, DemoPager } from "../../components/demo-page";
import { CompiledOutput } from "../../components/compiled-output";

export default function Page() {
  return (
    <article class="demo-page">
      <DemoHead slug="compiled" />
      <div class="demo-body">
        <section class="demo-stage" aria-label="Live demo"><Demo /></section>
        <DemoNotes slug="compiled" />
      </div>
      <CompiledOutput />
      <DemoSource slug="compiled" />
      <DemoPager slug="compiled" />
    </article>
  );
}
