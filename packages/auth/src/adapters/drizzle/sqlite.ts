import { integer, primaryKey, sqliteTable, text } from "drizzle-orm/sqlite-core";
import { resolveTableNames, type TableNames } from "./names.js";

export function createSqliteSchema(names?: Partial<TableNames>) {
  const n = resolveTableNames(names);

  const users = sqliteTable(n.users, {
    id: text("id").primaryKey(),
    name: text("name"),
    email: text("email").unique(),
    emailVerified: integer("emailVerified", { mode: "timestamp_ms" }),
    image: text("image"),
    role: text("role"),
    twoFactorEnabled: integer("twoFactorEnabled", { mode: "boolean" }).notNull().default(false),
    createdAt: integer("createdAt", { mode: "timestamp_ms" }).$defaultFn(() => new Date()),
  });

  const accounts = sqliteTable(
    n.accounts,
    {
      userId: text("userId").notNull().references(() => users.id, { onDelete: "cascade" }),
      type: text("type").notNull(),
      provider: text("provider").notNull(),
      providerAccountId: text("providerAccountId").notNull(),
      refresh_token: text("refresh_token"),
      access_token: text("access_token"),
      expires_at: integer("expires_at"),
      token_type: text("token_type"),
      scope: text("scope"),
      id_token: text("id_token"),
      session_state: text("session_state"),
    },
    (t) => [primaryKey({ columns: [t.provider, t.providerAccountId] })],
  );

  const sessions = sqliteTable(n.sessions, {
    sessionToken: text("sessionToken").primaryKey(),
    userId: text("userId").notNull().references(() => users.id, { onDelete: "cascade" }),
    expires: integer("expires", { mode: "timestamp_ms" }).notNull(),
  });

  const verificationTokens = sqliteTable(
    n.verificationTokens,
    {
      identifier: text("identifier").notNull(),
      token: text("token").notNull(),
      expires: integer("expires", { mode: "timestamp_ms" }).notNull(),
    },
    (t) => [primaryKey({ columns: [t.identifier, t.token] })],
  );

  const authenticators = sqliteTable(n.authenticators, {
    credentialID: text("credentialID").primaryKey(),
    userId: text("userId").notNull().references(() => users.id, { onDelete: "cascade" }),
    providerAccountId: text("providerAccountId").notNull(),
    credentialPublicKey: text("credentialPublicKey").notNull(),
    counter: integer("counter").notNull(),
    credentialDeviceType: text("credentialDeviceType").notNull(),
    credentialBackedUp: integer("credentialBackedUp", { mode: "boolean" }).notNull(),
    transports: text("transports"),
    name: text("name"),
    createdAt: integer("createdAt", { mode: "timestamp_ms" }).$defaultFn(() => new Date()),
  });

  const twoFactors = sqliteTable(n.twoFactors, {
    userId: text("userId").primaryKey().references(() => users.id, { onDelete: "cascade" }),
    secret: text("secret").notNull(),
    backupCodes: text("backupCodes").notNull(),
    confirmed: integer("confirmed", { mode: "boolean" }).notNull().default(false),
    lastUsedCounter: integer("lastUsedCounter"),
    createdAt: integer("createdAt", { mode: "timestamp_ms" }).$defaultFn(() => new Date()),
  });

  return { users, accounts, sessions, verificationTokens, authenticators, twoFactors };
}

export type SqliteAuthSchema = ReturnType<typeof createSqliteSchema>;
export type { TableNames };
