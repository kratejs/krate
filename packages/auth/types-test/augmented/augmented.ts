// Compiled with the augmentation next to it: role narrows, new fields appear,
// and the fields that were not redeclared keep their types.
import type { DefaultSession, Session, User } from "../../src/types.js";
import type { Equal, Expect } from "../helpers.js";

export type Checks = [
  Expect<Equal<Session["user"]["role"], "admin" | "member">>,
  Expect<Equal<Session["user"]["credits"], number>>,
  Expect<Equal<Session["user"]["banned"], boolean | undefined>>,
  Expect<Equal<Session["user"]["id"], string>>,
  Expect<Equal<Session["user"]["twoFactorEnabled"], boolean | undefined>>,
  Expect<Equal<User["role"], "admin" | "member">>,
  // DefaultSession stays the unaugmented base, like next-auth's.
  Expect<Equal<NonNullable<DefaultSession["user"]>["role"], string | null | undefined>>,
];

export const session: Session = { user: { id: "1", role: "admin", credits: 3 }, expires: "x" };

// @ts-expect-error role is narrowed
export const bad: Session = { user: { id: "1", role: "root", credits: 3 }, expires: "x" };
