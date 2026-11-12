import { randomId } from "../bytes.js";
import type { Account, Adapter, AdapterSession, AdapterUser, Authenticator, TwoFactorRecord, VerificationToken } from "../types.js";

export interface MemoryStore {
  users: Map<string, AdapterUser>;
  accounts: Account[];
  sessions: Map<string, AdapterSession>;
  tokens: VerificationToken[];
  authenticators: Map<string, Authenticator>;
  twoFactors: Map<string, TwoFactorRecord>;
}

export function createMemoryStore(): MemoryStore {
  return { users: new Map(), accounts: [], sessions: new Map(), tokens: [], authenticators: new Map(), twoFactors: new Map() };
}

export function MemoryAdapter(store: MemoryStore = createMemoryStore()): Adapter & { store: MemoryStore } {
  const clone = <T>(value: T): T => structuredClone(value);
  return {
    store,
    createUser(data) {
      const email = typeof data.email === "string" ? data.email.toLowerCase() : null;
      if (email) for (const u of store.users.values()) if (u.email === email) throw new Error("a user with that email already exists");
      const user: AdapterUser = { ...data, id: randomId(), email, emailVerified: (data.emailVerified ?? null) as Date | null };
      store.users.set(user.id, user);
      return clone(user);
    },
    getUser(id) {
      const user = store.users.get(id);
      return user ? clone(user) : null;
    },
    getUserByEmail(email) {
      const wanted = email.toLowerCase();
      for (const user of store.users.values()) if (user.email && user.email.toLowerCase() === wanted) return clone(user);
      return null;
    },
    getUserByAccount({ provider, providerAccountId }) {
      const account = store.accounts.find((a) => a.provider === provider && a.providerAccountId === providerAccountId);
      if (!account?.userId) return null;
      const user = store.users.get(account.userId);
      return user ? clone(user) : null;
    },
    updateUser(data) {
      const existing = store.users.get(data.id);
      if (!existing) throw new Error("user not found");
      const next = { ...existing, ...data };
      if (typeof next.email === "string") next.email = next.email.toLowerCase();
      store.users.set(next.id, next);
      return clone(next);
    },
    deleteUser(userId) {
      store.users.delete(userId);
      store.accounts = store.accounts.filter((a) => a.userId !== userId);
      for (const [token, session] of store.sessions) if (session.userId === userId) store.sessions.delete(token);
      for (const [id, auth] of store.authenticators) if (auth.userId === userId) store.authenticators.delete(id);
      store.twoFactors.delete(userId);
    },
    linkAccount(account) {
      if (store.accounts.some((a) => a.provider === account.provider && a.providerAccountId === account.providerAccountId)) {
        throw new Error("that provider account is already linked");
      }
      store.accounts.push(clone(account));
      return clone(account);
    },
    unlinkAccount({ provider, providerAccountId }) {
      store.accounts = store.accounts.filter((a) => !(a.provider === provider && a.providerAccountId === providerAccountId));
    },
    getAccount(providerAccountId, provider) {
      const account = store.accounts.find((a) => a.provider === provider && a.providerAccountId === providerAccountId);
      return account ? clone(account) : null;
    },
    listAccounts(userId) {
      return store.accounts.filter((a) => a.userId === userId).map(clone);
    },
    createSession(session) {
      store.sessions.set(session.sessionToken, clone(session));
      return clone(session);
    },
    getSessionAndUser(sessionToken) {
      const session = store.sessions.get(sessionToken);
      if (!session) return null;
      const user = store.users.get(session.userId);
      if (!user) return null;
      return { session: clone(session), user: clone(user) };
    },
    updateSession(data) {
      const existing = store.sessions.get(data.sessionToken);
      if (!existing) return null;
      const next = { ...existing, ...data };
      store.sessions.set(next.sessionToken, next);
      return clone(next);
    },
    deleteSession(sessionToken) {
      store.sessions.delete(sessionToken);
    },
    createVerificationToken(token) {
      store.tokens.push(clone(token));
      return clone(token);
    },
    useVerificationToken({ identifier, token }) {
      const index = store.tokens.findIndex((t) => t.identifier === identifier && t.token === token);
      if (index < 0) return null;
      const [found] = store.tokens.splice(index, 1);
      return found ?? null;
    },
    getAuthenticator(credentialID) {
      const found = store.authenticators.get(credentialID);
      return found ? clone(found) : null;
    },
    listAuthenticatorsByUserId(userId) {
      return [...store.authenticators.values()].filter((a) => a.userId === userId).map(clone);
    },
    createAuthenticator(authenticator) {
      store.authenticators.set(authenticator.credentialID, clone({ ...authenticator, createdAt: authenticator.createdAt ?? new Date() }));
      return clone(authenticator);
    },
    updateAuthenticatorCounter(credentialID, counter) {
      const existing = store.authenticators.get(credentialID);
      if (!existing) throw new Error("authenticator not found");
      existing.counter = counter;
      return clone(existing);
    },
    deleteAuthenticator(credentialID) {
      store.authenticators.delete(credentialID);
    },
    getTwoFactor(userId) {
      const found = store.twoFactors.get(userId);
      return found ? clone(found) : null;
    },
    upsertTwoFactor(record) {
      store.twoFactors.set(record.userId, clone({ ...record, createdAt: record.createdAt ?? new Date() }));
      return clone(record);
    },
    deleteTwoFactor(userId) {
      store.twoFactors.delete(userId);
    },
  };
}
