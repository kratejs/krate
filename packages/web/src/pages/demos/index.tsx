import { demos } from "../../demos/registry";
import "../../demos/kit.css";
import "../../styles/demos.css";

export default function Demos() {
  return (
    <div class="demos-index">
      <Head>
        <title>Live demos</title>
        <meta name="description" content="Eleven live, editable demos of what Krate can do: fine-grained signals, zero-JS CSS signals, view transitions, canvas, forms, data fetching and more." />
      </Head>
      <header class="demos-hero">
        <h1>Live demos</h1>
        <p>
          Everything here runs in your browser from a static site: no server, no API. Each demo
          shows its source and opens in the <a href="/playground/">playground</a>, where you can edit it and
          see the HTML and JavaScript Krate generates.
        </p>
      </header>
      <ul class="demos-grid">
        {demos.map((d) => (
          <li>
            <a class="demo-card" href={"/demos/" + d.slug + "/"}>
              <span class="demo-card-top" data-view-transition={"demo-" + d.slug}>
                <span class="demo-card-icon"><Icon name={d.icon} width="22" height="22" /></span>
                <span class={d.js === "zero" ? "demo-chip demo-chip-zero" : "demo-chip demo-chip-tag"}>
                  {d.js === "zero" ? "0 JS" : "Island"}
                </span>
              </span>
              <h2>{d.title}</h2>
              <p>{d.blurb}</p>
              <span class="demo-card-tags">{d.tags.map((t) => <span>{t}</span>)}</span>
            </a>
          </li>
        ))}
      </ul>
    </div>
  );
}
