import Detail from "../../../demos/transitions/detail";
import { swatches } from "../../../demos/transitions/swatches";
import "../../../demos/kit.css";
import "../../../styles/demos.css";

export function generateStaticParams() {
  return swatches.map((s) => ({ id: s.id }));
}

export const dynamicParams = false;

export default function DetailPage(props: { params?: { id: string } }) {
  const id = props.params?.id ?? "sunrise";
  const s = swatches.find((x) => x.id === id) ?? swatches[0];
  return (
    <div class="demo-page">
      <Head>
        <title>{s.name + " - view transitions demo"}</title>
        <meta name="description" content={"The " + s.name + " detail page of the view transitions demo."} />
      </Head>
      <nav class="demo-crumbs" aria-label="Breadcrumb">
        <a href="/demos/transitions/">Back to the demo</a>
      </nav>
      <h1 class="sr-only">{s.name}</h1>
      <section class="demo-stage" aria-label="Detail">
        <Detail id={id} />
      </section>
    </div>
  );
}
