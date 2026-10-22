import { useState } from "react";
import { log } from "../lib/log";

// <button formAction={fn}> overrides the form's action for that submitter.
export default function ActionButtonFormAction() {
  const [status, setStatus] = useState("draft");
  async function save(formData: FormData) {
    log("save", formData.get("title"));
    setStatus("saved " + formData.get("title"));
  }
  async function publish(formData: FormData) {
    log("publish", formData.get("title"));
    setStatus("published " + formData.get("title"));
  }
  return (
    <form action={save}>
      <input id="title" name="title" defaultValue="Post" />
      <button id="save" type="submit">
        save
      </button>
      <button id="publish" type="submit" formAction={publish}>
        publish
      </button>
      <p id="status">{status}</p>
    </form>
  );
}
