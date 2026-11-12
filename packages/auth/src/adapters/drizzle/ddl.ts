import { resolveTableNames, type TableNames } from "./names.js";

export type Dialect = "sqlite" | "postgres" | "mysql";

function q(dialect: Dialect, name: string): string {
  return dialect === "mysql" ? `\`${name}\`` : `"${name}"`;
}

export function authTablesSql(dialect: Dialect, names?: Partial<TableNames>): string[] {
  const n = resolveTableNames(names);
  const Q = (name: string) => q(dialect, name);
  const text = dialect === "mysql" ? "VARCHAR(255)" : "TEXT";
  const longText = "TEXT";
  const int = dialect === "sqlite" ? "INTEGER" : dialect === "postgres" ? "INTEGER" : "INT";
  const bool = dialect === "sqlite" ? "INTEGER" : dialect === "postgres" ? "BOOLEAN" : "BOOLEAN";
  const falseValue = dialect === "sqlite" ? "0" : "FALSE";
  const time = dialect === "sqlite" ? "INTEGER" : dialect === "postgres" ? "TIMESTAMPTZ" : "TIMESTAMP(3)";
  const nullTime = dialect === "mysql" ? `${time} NULL` : time;
  const credentialId = dialect === "mysql" ? "VARCHAR(512)" : "TEXT";
  const fk = (column: string) => `FOREIGN KEY (${Q(column)}) REFERENCES ${Q(n.users)}(${Q("id")}) ON DELETE CASCADE`;
  const now = dialect === "sqlite" ? "" : dialect === "postgres" ? " DEFAULT NOW()" : " DEFAULT CURRENT_TIMESTAMP(3)";

  return [
    `CREATE TABLE IF NOT EXISTS ${Q(n.users)} (
  ${Q("id")} ${text} PRIMARY KEY NOT NULL,
  ${Q("name")} ${text},
  ${Q("email")} ${text} UNIQUE,
  ${Q("emailVerified")} ${nullTime},
  ${Q("image")} ${longText},
  ${Q("role")} ${dialect === "mysql" ? "VARCHAR(64)" : "TEXT"},
  ${Q("twoFactorEnabled")} ${bool} NOT NULL DEFAULT ${falseValue},
  ${Q("createdAt")} ${nullTime}${now}
)`,
    `CREATE TABLE IF NOT EXISTS ${Q(n.accounts)} (
  ${Q("userId")} ${text} NOT NULL,
  ${Q("type")} ${dialect === "mysql" ? "VARCHAR(32)" : "TEXT"} NOT NULL,
  ${Q("provider")} ${text} NOT NULL,
  ${Q("providerAccountId")} ${text} NOT NULL,
  ${Q("refresh_token")} ${longText},
  ${Q("access_token")} ${longText},
  ${Q("expires_at")} ${int},
  ${Q("token_type")} ${dialect === "mysql" ? "VARCHAR(64)" : "TEXT"},
  ${Q("scope")} ${longText},
  ${Q("id_token")} ${longText},
  ${Q("session_state")} ${text},
  PRIMARY KEY (${Q("provider")}, ${Q("providerAccountId")}),
  ${fk("userId")}
)`,
    `CREATE TABLE IF NOT EXISTS ${Q(n.sessions)} (
  ${Q("sessionToken")} ${text} PRIMARY KEY NOT NULL,
  ${Q("userId")} ${text} NOT NULL,
  ${Q("expires")} ${time} NOT NULL,
  ${fk("userId")}
)`,
    `CREATE TABLE IF NOT EXISTS ${Q(n.verificationTokens)} (
  ${Q("identifier")} ${text} NOT NULL,
  ${Q("token")} ${text} NOT NULL,
  ${Q("expires")} ${time} NOT NULL,
  PRIMARY KEY (${Q("identifier")}, ${Q("token")})
)`,
    `CREATE TABLE IF NOT EXISTS ${Q(n.authenticators)} (
  ${Q("credentialID")} ${credentialId} PRIMARY KEY NOT NULL,
  ${Q("userId")} ${text} NOT NULL,
  ${Q("providerAccountId")} ${text} NOT NULL,
  ${Q("credentialPublicKey")} ${longText} NOT NULL,
  ${Q("counter")} ${int} NOT NULL,
  ${Q("credentialDeviceType")} ${dialect === "mysql" ? "VARCHAR(32)" : "TEXT"} NOT NULL,
  ${Q("credentialBackedUp")} ${bool} NOT NULL,
  ${Q("transports")} ${text},
  ${Q("name")} ${text},
  ${Q("createdAt")} ${nullTime}${now},
  ${fk("userId")}
)`,
    `CREATE TABLE IF NOT EXISTS ${Q(n.twoFactors)} (
  ${Q("userId")} ${text} PRIMARY KEY NOT NULL,
  ${Q("secret")} ${longText} NOT NULL,
  ${Q("backupCodes")} ${longText} NOT NULL,
  ${Q("confirmed")} ${bool} NOT NULL DEFAULT ${falseValue},
  ${Q("createdAt")} ${nullTime}${now},
  ${fk("userId")}
)`,
  ];
}
