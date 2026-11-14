import { AuthError, type RequestContext } from "./config.js";
import { readSession } from "./session.js";
import type { Account, AdapterUser, User } from "./types.js";

export interface ResolvedUser {
  user: AdapterUser | User;
  account: Account;
  isNewUser: boolean;
}

export async function resolveOAuthUser(
  ctx: RequestContext,
  profileUser: User,
  account: Account,
  profile: Record<string, any>,
  allowDangerousLinking: boolean,
): Promise<ResolvedUser> {
  const { config } = ctx;
  const adapter = config.adapter;
  if (!adapter) {
    return { user: { ...profileUser, id: String(profileUser.id ?? account.providerAccountId) }, account, isNewUser: false };
  }

  const linked = await adapter.getUserByAccount({ provider: account.provider, providerAccountId: account.providerAccountId });
  if (linked) return { user: linked, account: { ...account, userId: linked.id }, isNewUser: false };

  const current = await readSession(ctx).catch(() => null);
  if (current?.session) {
    const owner = await adapter.getUser(current.session.user.id);
    if (owner) {
      await adapter.linkAccount({ ...account, userId: owner.id });
      if (config.events.linkAccount) await config.events.linkAccount({ user: owner, account, profile });
      return { user: owner, account: { ...account, userId: owner.id }, isNewUser: false };
    }
  }

  const email = profileUser.email ? String(profileUser.email).toLowerCase() : null;
  if (email) {
    const existing = await adapter.getUserByEmail(email);
    if (existing) {
      if (!allowDangerousLinking && !config.linking.automatic) {
        throw new AuthError("OAuthAccountNotLinked", "email already belongs to a user with another sign in method", 403);
      }
      await adapter.linkAccount({ ...account, userId: existing.id });
      if (config.events.linkAccount) await config.events.linkAccount({ user: existing, account, profile });
      return { user: existing, account: { ...account, userId: existing.id }, isNewUser: false };
    }
  }

  const { id: _ignored, ...rest } = profileUser;
  const created = await adapter.createUser({
    ...rest,
    email,
    emailVerified: (profileUser as { emailVerified?: Date | null }).emailVerified ?? null,
  } as Omit<AdapterUser, "id">);
  await adapter.linkAccount({ ...account, userId: created.id });
  if (config.events.createUser) await config.events.createUser({ user: created });
  if (config.events.linkAccount) await config.events.linkAccount({ user: created, account, profile });
  return { user: created, account: { ...account, userId: created.id }, isNewUser: true };
}

export async function resolveEmailUser(ctx: RequestContext, email: string, providerId: string): Promise<ResolvedUser> {
  const adapter = ctx.config.adapter;
  if (!adapter) throw new AuthError("Configuration", "the email provider needs an adapter", 500);
  let user = await adapter.getUserByEmail(email);
  let isNewUser = false;
  if (!user) {
    user = await adapter.createUser({ email, emailVerified: new Date(), name: null, image: null });
    isNewUser = true;
    if (ctx.config.events.createUser) await ctx.config.events.createUser({ user });
  } else if (!user.emailVerified) {
    user = await adapter.updateUser({ id: user.id, emailVerified: new Date() });
    if (ctx.config.events.updateUser) await ctx.config.events.updateUser({ user });
  }
  const account: Account = { type: "email", provider: providerId, providerAccountId: email, userId: user.id };
  if (isNewUser) await adapter.linkAccount({ ...account, userId: user.id });
  return { user, account, isNewUser };
}
