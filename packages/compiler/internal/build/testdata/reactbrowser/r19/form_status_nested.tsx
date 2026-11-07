import { useState } from "react";
import { useFormStatus } from "react-dom";
import { createGate, log } from "../lib/log";

function Status() {
  const { pending, data } = useFormStatus();
  return <p id="status">{pending ? "pending:" + String(data?.get("name")) : "idle"}</p>;
}
function Button() {
  const { pending } = useFormStatus();
  return <button id="submit" type="submit" disabled={pending}>{pending ? "..." : "save"}</button>;
}
function Outside() {
  const { pending } = useFormStatus();
  return <p id="outside">{pending ? "outside pending" : "outside idle"}</p>;
}

export default function FormStatusNested() {
  const [saved, setSaved] = useState("none");
  async function save(fd: FormData) {
    const g = createGate();
    (window as any).__g = g;
    await g.promise;
    log("saved", String(fd.get("name")));
    setSaved(String(fd.get("name")));
  }
  return (
    <div>
      <form action={save} id="f">
        <input name="name" id="name" defaultValue="x" />
        <Status />
        <Button />
      </form>
      <Outside />
      <p id="saved">{saved}</p>
    </div>
  );
}
