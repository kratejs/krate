import { PGlite } from "@electric-sql/pglite";
import { createClient } from "@libsql/client";
import { getTableName } from "drizzle-orm";
import { drizzle as drizzleLibsql } from "drizzle-orm/libsql";
import { drizzle as drizzlePglite } from "drizzle-orm/pglite";
import { describe, expect, it } from "vitest";
import { runAdapterSuite } from "../../adapter-suite";
import { MemoryAdapter } from "../memory";
import { DrizzleAdapter } from "./index";
import { authTablesSql } from "./ddl";
import { createMysqlSchema } from "./mysql";
import { createPgSchema } from "./pg";
import { createSqliteSchema } from "./sqlite";

runAdapterSuite("memory", () => MemoryAdapter());

runAdapterSuite("drizzle sqlite (libsql)", async () => {
  const client = createClient({ url: ":memory:" });
  await client.execute("PRAGMA foreign_keys = ON");
  for (const statement of authTablesSql("sqlite")) await client.execute(statement);
  return DrizzleAdapter(drizzleLibsql(client), createSqliteSchema());
});

runAdapterSuite("drizzle postgres (pglite)", async () => {
  const client = new PGlite();
  for (const statement of authTablesSql("postgres")) await client.exec(statement);
  return DrizzleAdapter(drizzlePglite(client), createPgSchema());
});

describe("custom table names", () => {
  it("applies names to the schema and the ddl", async () => {
    const names = { users: "app_users", accounts: "app_accounts", sessions: "app_sessions", verificationTokens: "app_tokens", authenticators: "app_passkeys", twoFactors: "app_2fa" };
    const schema = createSqliteSchema(names);
    expect(getTableName(schema.users)).toBe("app_users");
    expect(getTableName(schema.authenticators)).toBe("app_passkeys");
    const client = createClient({ url: ":memory:" });
    for (const statement of authTablesSql("sqlite", names)) await client.execute(statement);
    const adapter = DrizzleAdapter(drizzleLibsql(client), schema);
    const user = await adapter.createUser({ email: "x@example.com", emailVerified: null });
    expect((await adapter.getUser(user.id))?.email).toBe("x@example.com");
    const rows = await client.execute("SELECT count(*) AS n FROM app_users");
    expect(Number(rows.rows[0]!.n)).toBe(1);
  });

  it("reads names injected from krate.config", () => {
    (globalThis as any).__KRATE_AUTH_CONFIG__ = { adapter: { dialect: "sqlite", tables: { users: "configured_users" } } };
    try {
      expect(getTableName(createSqliteSchema().users)).toBe("configured_users");
      expect(getTableName(createPgSchema().users)).toBe("configured_users");
      expect(getTableName(createSqliteSchema({ users: "explicit" }).users)).toBe("explicit");
    } finally {
      delete (globalThis as any).__KRATE_AUTH_CONFIG__;
    }
    expect(getTableName(createSqliteSchema().users)).toBe("user");
  });
});

describe("mysql", () => {
  it("builds a schema and ddl without needing a server", () => {
    const schema = createMysqlSchema();
    expect(getTableName(schema.users)).toBe("user");
    const sql = authTablesSql("mysql").join("\n");
    expect(sql).toContain("`user`");
    expect(sql).toContain("VARCHAR(255)");
    expect(sql).toContain("ON DELETE CASCADE");
  });

  it("refuses a schema with missing tables", () => {
    expect(() => DrizzleAdapter({}, { users: {} } as any)).toThrow("schema.accounts");
  });
});
