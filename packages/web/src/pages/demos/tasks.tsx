import Demo from "../../demos/tasks/demo";
import { DemoHead, DemoNotes, DemoSource, DemoPager } from "../../components/demo-page";

export default function Page() {
  return (
    <article class="demo-page">
      <DemoHead slug="tasks" />
      <div class="demo-body">
        <section class="demo-stage" aria-label="Live demo"><Demo /></section>
        <DemoNotes slug="tasks" />
      </div>
      <DemoSource slug="tasks" />
      <DemoPager slug="tasks" />
    </article>
  );
}
