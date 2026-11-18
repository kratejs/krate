// Compiled by `pnpm typecheck` with no augmentation: the session types are the
// ones @krate/auth always had.
import type { AuthOptions, BaseSessionUser, DefaultSession, Session, User } from "../../src/types.js";
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

// An Auth.js adapter (@auth/*-adapter) marks the user and account methods optional.
interface CoreAdapterUser {
  id: string;
  email: string;
  emailVerified: Date | null;
  name?: string | null;
  image?: string | null;
}
interface AuthJsStyleAdapter {
  createUser?(user: CoreAdapterUser): Promise<CoreAdapterUser> | CoreAdapterUser;
  getUser?(id: string): Promise<CoreAdapterUser | null> | CoreAdapterUser | null;
  getUserByEmail?(email: string): Promise<CoreAdapterUser | null> | CoreAdapterUser | null;
  getUserByAccount?(account: { provider: string; providerAccountId: string }): Promise<CoreAdapterUser | null> | CoreAdapterUser | null;
  updateUser?(user: Partial<CoreAdapterUser> & Pick<CoreAdapterUser, "id">): Promise<CoreAdapterUser> | CoreAdapterUser;
  linkAccount?(account: { userId: string; type: "oauth" | "oidc" | "email" | "webauthn"; provider: string; providerAccountId: string }): Promise<void> | void;
}
declare const authJsAdapter: AuthJsStyleAdapter;
export const options: AuthOptions = { adapter: authJsAdapter };
