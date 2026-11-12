import { boolean, integer, pgTable, primaryKey, text, timestamp } from "drizzle-orm/pg-core";
import { resolveTableNames, type TableNames } from "./names.js";

export function createPgSchema(names?: Partial<TableNames>) {
  const n = resolveTableNames(names);

  const users = pgTable(n.users, {
    id: text("id").primaryKey(),
    name: text("name"),
    email: text("email").unique(),
    emailVerified: timestamp("emailVerified", { mode: "date", withTimezone: true }),
    image: text("image"),
    role: text("role"),
    twoFactorEnabled: boolean("twoFactorEnabled").notNull().default(false),
    createdAt: timestamp("createdAt", { mode: "date", withTimezone: true }).defaultNow(),
  });

  const accounts = pgTable(
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

  const sessions = pgTable(n.sessions, {
    sessionToken: text("sessionToken").primaryKey(),
    userId: text("userId").notNull().references(() => users.id, { onDelete: "cascade" }),
    expires: timestamp("expires", { mode: "date", withTimezone: true }).notNull(),
  });

  const verificationTokens = pgTable(
    n.verificationTokens,
    {
      identifier: text("identifier").notNull(),
      token: text("token").notNull(),
      expires: timestamp("expires", { mode: "date", withTimezone: true }).notNull(),
    },
    (t) => [primaryKey({ columns: [t.identifier, t.token] })],
  );

  const authenticators = pgTable(n.authenticators, {
    credentialID: text("credentialID").primaryKey(),
    userId: text("userId").notNull().references(() => users.id, { onDelete: "cascade" }),
    providerAccountId: text("providerAccountId").notNull(),
    credentialPublicKey: text("credentialPublicKey").notNull(),
    counter: integer("counter").notNull(),
    credentialDeviceType: text("credentialDeviceType").notNull(),
    credentialBackedUp: boolean("credentialBackedUp").notNull(),
    transports: text("transports"),
    name: text("name"),
    createdAt: timestamp("createdAt", { mode: "date", withTimezone: true }).defaultNow(),
  });

  const twoFactors = pgTable(n.twoFactors, {
    userId: text("userId").primaryKey().references(() => users.id, { onDelete: "cascade" }),
    secret: text("secret").notNull(),
    backupCodes: text("backupCodes").notNull(),
    confirmed: boolean("confirmed").notNull().default(false),
    createdAt: timestamp("createdAt", { mode: "date", withTimezone: true }).defaultNow(),
  });

  return { users, accounts, sessions, verificationTokens, authenticators, twoFactors };
}

export type PgAuthSchema = ReturnType<typeof createPgSchema>;
export type { TableNames };
