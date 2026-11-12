import { beforeEach, describe, expect, it } from "vitest";
import type { Adapter } from "./types";

export function runAdapterSuite(name: string, make: () => Promise<Adapter> | Adapter): void {
  describe(`adapter: ${name}`, () => {
    let adapter: Adapter;
    beforeEach(async () => {
      adapter = await make();
    });

    it("creates and reads users", async () => {
      const created = await adapter.createUser({ name: "Ada", email: "Ada@Example.com", emailVerified: null, image: "i.png" });
      expect(created.id).toBeTruthy();
      expect(created.email).toBe("ada@example.com");
      expect(created.emailVerified).toBeNull();
      expect(await adapter.getUser(created.id)).toMatchObject({ id: created.id, name: "Ada", image: "i.png" });
      expect((await adapter.getUserByEmail("ADA@example.com"))?.id).toBe(created.id);
      expect(await adapter.getUserByEmail("nobody@example.com")).toBeNull();
      expect(await adapter.getUser("missing")).toBeNull();
    });

    it("stores the verified date as a Date", async () => {
      const when = new Date("2026-01-02T03:04:05.000Z");
      const user = await adapter.createUser({ email: "v@example.com", emailVerified: when, name: null });
      const read = await adapter.getUser(user.id);
      expect(read?.emailVerified).toBeInstanceOf(Date);
      expect(read?.emailVerified?.toISOString()).toBe(when.toISOString());
    });

    it("rejects duplicate emails", async () => {
      await adapter.createUser({ email: "dup@example.com", emailVerified: null });
      await expect(Promise.resolve().then(() => adapter.createUser({ email: "DUP@example.com", emailVerified: null }))).rejects.toThrow();
    });

    it("updates users", async () => {
      const user = await adapter.createUser({ name: "Old", email: "u@example.com", emailVerified: null });
      const updated = await adapter.updateUser({ id: user.id, name: "New", role: "admin", twoFactorEnabled: true });
      expect(updated).toMatchObject({ name: "New", role: "admin", twoFactorEnabled: true, email: "u@example.com" });
      const verified = await adapter.updateUser({ id: user.id, emailVerified: new Date() });
      expect(verified.emailVerified).toBeInstanceOf(Date);
    });

    it("links accounts and finds users by account", async () => {
      const user = await adapter.createUser({ email: "l@example.com", emailVerified: null });
      await adapter.linkAccount({ userId: user.id, type: "oauth", provider: "github", providerAccountId: "42", access_token: "at", refresh_token: "rt", expires_at: 1_900_000_000, token_type: "bearer", scope: "read", id_token: "idt", session_state: "ss" });
      expect((await adapter.getUserByAccount({ provider: "github", providerAccountId: "42" }))?.id).toBe(user.id);
      expect(await adapter.getUserByAccount({ provider: "github", providerAccountId: "43" })).toBeNull();
      expect(await adapter.getAccount?.("42", "github")).toMatchObject({ access_token: "at", refresh_token: "rt", expires_at: 1_900_000_000, scope: "read" });
      expect(await adapter.listAccounts?.(user.id)).toHaveLength(1);
      await adapter.unlinkAccount?.({ provider: "github", providerAccountId: "42" });
      expect(await adapter.getUserByAccount({ provider: "github", providerAccountId: "42" })).toBeNull();
    });

    it("rejects linking the same provider account twice", async () => {
      const a = await adapter.createUser({ email: "a@example.com", emailVerified: null });
      const b = await adapter.createUser({ email: "b@example.com", emailVerified: null });
      await adapter.linkAccount({ userId: a.id, type: "oauth", provider: "github", providerAccountId: "1" });
      await expect(Promise.resolve().then(() => adapter.linkAccount({ userId: b.id, type: "oauth", provider: "github", providerAccountId: "1" }))).rejects.toThrow();
    });

    it("manages sessions", async () => {
      const user = await adapter.createUser({ email: "s@example.com", emailVerified: null, name: "S" });
      const expires = new Date(Date.now() + 3_600_000);
      await adapter.createSession!({ sessionToken: "tok-1", userId: user.id, expires });
      const found = await adapter.getSessionAndUser!("tok-1");
      expect(found?.user.id).toBe(user.id);
      expect(found?.session.expires.getTime()).toBe(expires.getTime());
      expect(await adapter.getSessionAndUser!("nope")).toBeNull();
      const later = new Date(Date.now() + 7_200_000);
      await adapter.updateSession!({ sessionToken: "tok-1", expires: later });
      expect((await adapter.getSessionAndUser!("tok-1"))?.session.expires.getTime()).toBe(later.getTime());
      await adapter.deleteSession!("tok-1");
      expect(await adapter.getSessionAndUser!("tok-1")).toBeNull();
    });

    it("consumes verification tokens exactly once", async () => {
      const expires = new Date(Date.now() + 60_000);
      await adapter.createVerificationToken!({ identifier: "e@example.com", token: "hash-1", expires });
      expect(await adapter.useVerificationToken!({ identifier: "e@example.com", token: "other" })).toBeNull();
      expect(await adapter.useVerificationToken!({ identifier: "x@example.com", token: "hash-1" })).toBeNull();
      const used = await adapter.useVerificationToken!({ identifier: "e@example.com", token: "hash-1" });
      expect(used).toMatchObject({ identifier: "e@example.com", token: "hash-1" });
      expect(used?.expires.getTime()).toBe(expires.getTime());
      expect(await adapter.useVerificationToken!({ identifier: "e@example.com", token: "hash-1" })).toBeNull();
    });

    it("stores authenticators", async () => {
      const user = await adapter.createUser({ email: "p@example.com", emailVerified: null });
      await adapter.createAuthenticator!({ credentialID: "cred-1", userId: user.id, providerAccountId: "cred-1", credentialPublicKey: "pk", counter: 0, credentialDeviceType: "multiDevice", credentialBackedUp: true, transports: "internal,hybrid", name: "Laptop" });
      const read = await adapter.getAuthenticator!("cred-1");
      expect(read).toMatchObject({ userId: user.id, credentialPublicKey: "pk", counter: 0, credentialDeviceType: "multiDevice", credentialBackedUp: true, transports: "internal,hybrid", name: "Laptop" });
      expect(await adapter.listAuthenticatorsByUserId!(user.id)).toHaveLength(1);
      expect((await adapter.updateAuthenticatorCounter!("cred-1", 5)).counter).toBe(5);
      await adapter.deleteAuthenticator!("cred-1");
      expect(await adapter.getAuthenticator!("cred-1")).toBeNull();
    });

    it("stores two-factor records", async () => {
      const user = await adapter.createUser({ email: "t@example.com", emailVerified: null });
      expect(await adapter.getTwoFactor!(user.id)).toBeNull();
      await adapter.upsertTwoFactor!({ userId: user.id, secret: "sealed", backupCodes: ["h1", "h2"], confirmed: false });
      expect(await adapter.getTwoFactor!(user.id)).toMatchObject({ secret: "sealed", backupCodes: ["h1", "h2"], confirmed: false });
      await adapter.upsertTwoFactor!({ userId: user.id, secret: "sealed", backupCodes: ["h2"], confirmed: true });
      expect(await adapter.getTwoFactor!(user.id)).toMatchObject({ backupCodes: ["h2"], confirmed: true });
      await adapter.deleteTwoFactor!(user.id);
      expect(await adapter.getTwoFactor!(user.id)).toBeNull();
    });

    it("deletes a user with everything attached", async () => {
      const user = await adapter.createUser({ email: "d@example.com", emailVerified: null });
      await adapter.linkAccount({ userId: user.id, type: "oauth", provider: "g", providerAccountId: "1" });
      await adapter.createSession!({ sessionToken: "t", userId: user.id, expires: new Date(Date.now() + 1000) });
      await adapter.createAuthenticator!({ credentialID: "c", userId: user.id, providerAccountId: "c", credentialPublicKey: "k", counter: 0, credentialDeviceType: "singleDevice", credentialBackedUp: false });
      await adapter.upsertTwoFactor!({ userId: user.id, secret: "s", backupCodes: [], confirmed: true });
      await adapter.deleteUser!(user.id);
      expect(await adapter.getUser(user.id)).toBeNull();
      expect(await adapter.getUserByAccount({ provider: "g", providerAccountId: "1" })).toBeNull();
      expect(await adapter.getSessionAndUser!("t")).toBeNull();
      expect(await adapter.getAuthenticator!("c")).toBeNull();
      expect(await adapter.getTwoFactor!(user.id)).toBeNull();
    });
  });
}
