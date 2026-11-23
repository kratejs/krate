"use client";
import { useActionState } from "react";

type State = { ok: boolean; msg: string } | null;
type Action = (prev: State, form: FormData) => Promise<NonNullable<State>>;

export function SaveForm({ id, action }: { id: string; action: Action }) {
  const [state, run, pending] = useActionState<State, FormData>(action, null);
  return (
    <form action={run}>
      <input name="name" defaultValue="x" />
      <button id={id} type="submit" disabled={pending}>Send</button>
      {state && <p id={id + "-status"} role="status">{state.msg}</p>}
    </form>
  );
}
