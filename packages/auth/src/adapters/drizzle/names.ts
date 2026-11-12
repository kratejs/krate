import { readDeclarative } from "../../config.js";

export interface TableNames {
  users: string;
  accounts: string;
  sessions: string;
  verificationTokens: string;
  authenticators: string;
  twoFactors: string;
}

export const defaultTableNames: TableNames = {
  users: "user",
  accounts: "account",
  sessions: "session",
  verificationTokens: "verificationToken",
  authenticators: "authenticator",
  twoFactors: "twoFactor",
};

export function resolveTableNames(overrides?: Partial<TableNames>): TableNames {
  const declared = (readDeclarative().adapter?.tables ?? {}) as Partial<TableNames>;
  return { ...defaultTableNames, ...declared, ...(overrides ?? {}) };
}
