import Demo from "../../demos/css-signals/demo";
import { DemoHead, DemoNotes, DemoSource, DemoPager } from "../../components/demo-page";

export default function Page() {
  return (
    <article class="demo-page">
      <DemoHead slug="css-signals" />
      <div class="demo-body">
        <section class="demo-stage" aria-label="Live demo"><Demo /></section>
        <DemoNotes slug="css-signals" />
      </div>
      <DemoSource slug="css-signals" />
      <DemoPager slug="css-signals" />
    </article>
  );
}
