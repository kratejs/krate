import Demo from "../../demos/chart/demo";
import { DemoHead, DemoNotes, DemoSource, DemoPager } from "../../components/demo-page";

export default function Page() {
  return (
    <article class="demo-page">
      <DemoHead slug="chart" />
      <div class="demo-body">
        <section class="demo-stage" aria-label="Live demo"><Demo /></section>
        <DemoNotes slug="chart" />
      </div>
      <DemoSource slug="chart" />
      <DemoPager slug="chart" />
    </article>
  );
}
