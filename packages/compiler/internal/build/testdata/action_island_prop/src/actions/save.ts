"use server";

type State = { ok: boolean; msg: string } | null;

export async function save(_prev: State, form: FormData) {
  return { ok: true, msg: "saved " + String(form.get("name")) };
}

export async function saveTagged(tag: string, _prev: State, form: FormData) {
  return { ok: true, msg: tag + ":" + String(form.get("name")) };
}
