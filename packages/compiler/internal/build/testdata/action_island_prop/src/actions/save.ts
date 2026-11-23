"use server";

import { redirect } from "@krate/runtime/server";

type State = { ok: boolean; msg: string } | null;

export async function save(_prev: State, form: FormData) {
  return { ok: true, msg: "saved " + String(form.get("name")) };
}

export async function saveTagged(tag: string, _prev: State, form: FormData) {
  return { ok: true, msg: tag + ":" + String(form.get("name")) };
}

export async function ping(form: FormData) {
  redirect("/done?n=" + encodeURIComponent(String(form.get("name"))));
}
