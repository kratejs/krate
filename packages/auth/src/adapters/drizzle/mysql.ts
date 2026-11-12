import { boolean, int, mysqlTable, primaryKey, text, timestamp, varchar } from "drizzle-orm/mysql-core";
import { resolveTableNames, type TableNames } from "./names.js";

export function createMysqlSchema(names?: Partial<TableNames>) {
  const n = resolveTableNames(names);

  const users = mysqlTable(n.users, {
    id: varchar("id", { length: 255 }).primaryKey(),
    name: varchar("name", { length: 255 }),
    email: varchar("email", { length: 255 }).unique(),
    emailVerified: timestamp("emailVerified", { mode: "date", fsp: 3 }),
    image: text("image"),
    role: varchar("role", { length: 64 }),
    twoFactorEnabled: boolean("twoFactorEnabled").notNull().default(false),
    createdAt: timestamp("createdAt", { mode: "date", fsp: 3 }).defaultNow(),
  });

  const accounts = mysqlTable(
    n.accounts,
    {
      userId: varchar("userId", { length: 255 }).notNull().references(() => users.id, { onDelete: "cascade" }),
      type: varchar("type", { length: 32 }).notNull(),
      provider: varchar("provider", { length: 255 }).notNull(),
      providerAccountId: varchar("providerAccountId", { length: 255 }).notNull(),
      refresh_token: text("refresh_token"),
      access_token: text("access_token"),
      expires_at: int("expires_at"),
      token_type: varchar("token_type", { length: 64 }),
      scope: text("scope"),
      id_token: text("id_token"),
      session_state: varchar("session_state", { length: 255 }),
    },
    (t) => [primaryKey({ columns: [t.provider, t.providerAccountId] })],
  );

  const sessions = mysqlTable(n.sessions, {
    sessionToken: varchar("sessionToken", { length: 255 }).primaryKey(),
    userId: varchar("userId", { length: 255 }).notNull().references(() => users.id, { onDelete: "cascade" }),
    expires: timestamp("expires", { mode: "date", fsp: 3 }).notNull(),
  });

  const verificationTokens = mysqlTable(
    n.verificationTokens,
    {
      identifier: varchar("identifier", { length: 255 }).notNull(),
      token: varchar("token", { length: 255 }).notNull(),
      expires: timestamp("expires", { mode: "date", fsp: 3 }).notNull(),
    },
    (t) => [primaryKey({ columns: [t.identifier, t.token] })],
  );

  const authenticators = mysqlTable(n.authenticators, {
    credentialID: varchar("credentialID", { length: 512 }).primaryKey(),
    userId: varchar("userId", { length: 255 }).notNull().references(() => users.id, { onDelete: "cascade" }),
    providerAccountId: varchar("providerAccountId", { length: 255 }).notNull(),
    credentialPublicKey: text("credentialPublicKey").notNull(),
    counter: int("counter").notNull(),
    credentialDeviceType: varchar("credentialDeviceType", { length: 32 }).notNull(),
    credentialBackedUp: boolean("credentialBackedUp").notNull(),
    transports: varchar("transports", { length: 255 }),
    name: varchar("name", { length: 255 }),
    createdAt: timestamp("createdAt", { mode: "date", fsp: 3 }).defaultNow(),
  });

  const twoFactors = mysqlTable(n.twoFactors, {
    userId: varchar("userId", { length: 255 }).primaryKey().references(() => users.id, { onDelete: "cascade" }),
    secret: text("secret").notNull(),
    backupCodes: text("backupCodes").notNull(),
    confirmed: boolean("confirmed").notNull().default(false),
    createdAt: timestamp("createdAt", { mode: "date", fsp: 3 }).defaultNow(),
  });

  return { users, accounts, sessions, verificationTokens, authenticators, twoFactors };
}

export type MysqlAuthSchema = ReturnType<typeof createMysqlSchema>;
export type { TableNames };
