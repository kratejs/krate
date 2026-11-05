import { useState } from "react";
import { JsGreeter } from "../lib/decorated-js.js";
import { OtherBox } from "../lib/decorated-other";
import { TsBox } from "../lib/decorated-ts";

// @ts-expect-error tier decorator
@static
const Label = ({ text }: { text: string }) => <b id="dec-static">{text}</b>;

export default function DecoratorsPage() {
  const [clicks, setClicks] = useState(0);
  const box = new TsBox();
  return (
    <main>
      <Label text="static label" />
      <p id="dec-js">{new JsGreeter().greet()}</p>
      <p id="dec-ts">{box.first} {box.second}</p>
      <p id="dec-other">{new OtherBox().value}</p>
      <button id="dec-button" onClick={() => setClicks(clicks + 1)}>{clicks}</button>
    </main>
  );
}
