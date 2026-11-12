import { and, eq, sql } from "drizzle-orm";
import { randomId } from "../../bytes.js";
import type { Account, Adapter, AdapterSession, AdapterUser, Authenticator, TwoFactorRecord, VerificationToken } from "../../types.js";

export interface AuthSchema {
  users: any;
  accounts: any;
  sessions: any;
  verificationTokens: any;
  authenticators: any;
  twoFactors: any;
}

const USER_FIELDS = ["name", "email", "emailVerified", "image", "role", "twoFactorEnabled"] as const;
const ACCOUNT_FIELDS = ["userId", "type", "provider", "providerAccountId", "refresh_token", "access_token", "expires_at", "token_type", "scope", "id_token", "session_state"] as const;

function first<T>(rows: T[] | undefined | null): T | null {
  return rows && rows.length > 0 ? rows[0]! : null;
}

function pick(source: Record<string, unknown>, fields: readonly string[]): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const f of fields) if (source[f] !== undefined) out[f] = source[f];
  return out;
}

function toUser(row: any): AdapterUser | null {
  if (!row) return null;
  return { ...row, emailVerified: row.emailVerified ?? null, email: row.email ?? null } as AdapterUser;
}

function toAccount(row: any): Account {
  return { ...row } as Account;
}

function toAuthenticator(row: any): Authenticator {
  return { ...row } as Authenticator;
}

export function DrizzleAdapter(db: any, schema: AuthSchema): Adapter {
  const { users, accounts, sessions, verificationTokens, authenticators, twoFactors } = schema;
  for (const [name, table] of Object.entries({ users, accounts, sessions, verificationTokens, authenticators, twoFactors })) {
    if (!table) throw new Error(`DrizzleAdapter: schema.${name} is missing`);
  }

  const userById = async (id: string) => toUser(first(await db.select().from(users).where(eq(users.id, id)).limit(1)));
  const accountWhere = (provider: string, providerAccountId: string) => and(eq(accounts.provider, provider), eq(accounts.providerAccountId, providerAccountId));

  return {
    async createUser(data) {
      const id = randomId();
      const values = pick(data as Record<string, unknown>, USER_FIELDS);
      if (typeof values.email === "string") values.email = values.email.toLowerCase();
      await db.insert(users).values({ id, ...values, emailVerified: data.emailVerified ?? null });
      return (await userById(id))!;
    },
    getUser: (id) => userById(id),
    async getUserByEmail(email) {
      return toUser(first(await db.select().from(users).where(eq(sql`lower(${users.email})`, email.toLowerCase())).limit(1)));
    },
    async getUserByAccount({ provider, providerAccountId }) {
      const account = first(await db.select().from(accounts).where(accountWhere(provider, providerAccountId)).limit(1)) as { userId: string } | null;
      return account ? userById(account.userId) : null;
    },
    async updateUser(data) {
      const values = pick(data as Record<string, unknown>, USER_FIELDS);
      if (typeof values.email === "string") values.email = values.email.toLowerCase();
      if (Object.keys(values).length > 0) await db.update(users).set(values).where(eq(users.id, data.id));
      const user = await userById(data.id);
      if (!user) throw new Error("user not found");
      return user;
    },
    async deleteUser(userId) {
      await db.delete(twoFactors).where(eq(twoFactors.userId, userId));
      await db.delete(authenticators).where(eq(authenticators.userId, userId));
      await db.delete(sessions).where(eq(sessions.userId, userId));
      await db.delete(accounts).where(eq(accounts.userId, userId));
      await db.delete(users).where(eq(users.id, userId));
    },
    async linkAccount(account) {
      const values = pick(account as Record<string, unknown>, ACCOUNT_FIELDS);
      await db.insert(accounts).values(values);
      return account;
    },
    async unlinkAccount({ provider, providerAccountId }) {
      await db.delete(accounts).where(accountWhere(provider, providerAccountId));
    },
    async getAccount(providerAccountId, provider) {
      const row = first(await db.select().from(accounts).where(accountWhere(provider, providerAccountId)).limit(1));
      return row ? toAccount(row) : null;
    },
    async listAccounts(userId) {
      return (await db.select().from(accounts).where(eq(accounts.userId, userId))).map(toAccount);
    },
    async createSession(session) {
      await db.insert(sessions).values(session);
      return session;
    },
    async getSessionAndUser(sessionToken) {
      const session = first(await db.select().from(sessions).where(eq(sessions.sessionToken, sessionToken)).limit(1)) as AdapterSession | null;
      if (!session) return null;
      const user = await userById(session.userId);
      return user ? { session, user } : null;
    },
    async updateSession(data) {
      const { sessionToken, ...values } = data;
      if (Object.keys(values).length > 0) await db.update(sessions).set(values).where(eq(sessions.sessionToken, sessionToken));
      return first(await db.select().from(sessions).where(eq(sessions.sessionToken, sessionToken)).limit(1)) as AdapterSession | null;
    },
    async deleteSession(sessionToken) {
      await db.delete(sessions).where(eq(sessions.sessionToken, sessionToken));
    },
    async createVerificationToken(token) {
      await db.insert(verificationTokens).values(token);
      return token;
    },
    async useVerificationToken({ identifier, token }) {
      const where = and(eq(verificationTokens.identifier, identifier), eq(verificationTokens.token, token));
      const del = db.delete(verificationTokens).where(where);
      if (typeof del.returning === "function") return (first(await del.returning()) as VerificationToken | null) ?? null;
      const row = first(await db.select().from(verificationTokens).where(where).limit(1)) as VerificationToken | null;
      if (row) await db.delete(verificationTokens).where(where);
      return row;
    },
    async getAuthenticator(credentialID) {
      const row = first(await db.select().from(authenticators).where(eq(authenticators.credentialID, credentialID)).limit(1));
      return row ? toAuthenticator(row) : null;
    },
    async listAuthenticatorsByUserId(userId) {
      return (await db.select().from(authenticators).where(eq(authenticators.userId, userId))).map(toAuthenticator);
    },
    async createAuthenticator(authenticator) {
      await db.insert(authenticators).values({ ...authenticator, transports: authenticator.transports ?? null, name: authenticator.name ?? null });
      return authenticator;
    },
    async updateAuthenticatorCounter(credentialID, counter) {
      await db.update(authenticators).set({ counter }).where(eq(authenticators.credentialID, credentialID));
      const row = first(await db.select().from(authenticators).where(eq(authenticators.credentialID, credentialID)).limit(1));
      if (!row) throw new Error("authenticator not found");
      return toAuthenticator(row);
    },
    async deleteAuthenticator(credentialID) {
      await db.delete(authenticators).where(eq(authenticators.credentialID, credentialID));
    },
    async getTwoFactor(userId) {
      const row = first(await db.select().from(twoFactors).where(eq(twoFactors.userId, userId)).limit(1)) as
        | { userId: string; secret: string; backupCodes: string; confirmed: boolean; createdAt?: Date }
        | null;
      if (!row) return null;
      return { ...row, backupCodes: JSON.parse(row.backupCodes) as string[] } as TwoFactorRecord;
    },
    async upsertTwoFactor(record) {
      const values = { userId: record.userId, secret: record.secret, backupCodes: JSON.stringify(record.backupCodes), confirmed: record.confirmed };
      const existing = first(await db.select().from(twoFactors).where(eq(twoFactors.userId, record.userId)).limit(1));
      if (existing) await db.update(twoFactors).set(values).where(eq(twoFactors.userId, record.userId));
      else await db.insert(twoFactors).values(values);
      return record;
    },
    async deleteTwoFactor(userId) {
      await db.delete(twoFactors).where(eq(twoFactors.userId, userId));
    },
  };
}

export { createSqliteSchema } from "./sqlite.js";
export { createPgSchema } from "./pg.js";
export { createMysqlSchema } from "./mysql.js";
export { defaultTableNames, resolveTableNames, type TableNames } from "./names.js";
