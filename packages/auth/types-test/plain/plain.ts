// Compiled by `pnpm typecheck` with no augmentation: the session types are the
// ones @krate/auth always had.
import type { BaseSessionUser, DefaultSession, Session, User } from "../../src/types.js";
import type { Equal, Expect } from "../helpers.js";

export type Checks = [
  Expect<Equal<Session["user"]["id"], string>>,
  Expect<Equal<Session["user"]["role"], string | null | undefined>>,
  Expect<Equal<Session["user"]["twoFactorEnabled"], boolean | undefined>>,
  Expect<Equal<Session["user"]["anything"], unknown>>,
  Expect<Equal<User["role"], string | null | undefined>>,
  Expect<Equal<DefaultSession["user"], BaseSessionUser | undefined>>,
];

export const session: Session = { user: { id: "1", name: null, plan: "pro" }, expires: "x" };
export const user: User = { email: "a@b.c", plan: "pro" };
