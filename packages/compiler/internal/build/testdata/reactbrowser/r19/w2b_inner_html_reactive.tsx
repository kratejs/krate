import { useState } from "react";

export default function InnerHtmlReactive() {
  const [html, setHtml] = useState("<b>one</b>");
  return (
    <div>
      <button id="b" onClick={() => setHtml("<i>two</i>")}>change</button>
      <div id="out" dangerouslySetInnerHTML={{ __html: html }} />
    </div>
  );
}
