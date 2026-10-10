/**
 * TypeScript types and a helper for authoring custom `krate check` rules.
 *
 * A custom rule module (referenced from `checks.custom` in krate.config.ts)
 * exports a `check(page, krate)` function returning findings. Modules run in
 * Krate's embedded QuickJS runtime at check time - no Node, no subprocess.
 *
 * ```ts
 * import { defineCheckRule } from "@krate/plugin";
 *
 * export default defineCheckRule((page) => {
 *   if (/lorem ipsum/i.test(page.html)) {
 *     return [{ rule: "custom/no-lorem", message: "Remove placeholder copy." }];
 *   }
 *   return [];
 * });
 * ```
 */

/** Severity a custom rule may report. */
export type CheckRuleSeverity = "error" | "warning";

/** One finding returned by a custom rule. */
export interface CheckFinding {
  /** Rule ID (e.g. "custom/no-lorem"). Defaults to `custom/<filename>`. */
  rule?: string;
  /** Human-readable problem description (required to be reported). */
  message: string;
  /** Defaults to "error". */
  severity?: CheckRuleSeverity;
  /** Source line, if known. */
  line?: number;
  /** Source column, if known. */
  col?: number;
  /** Optional remediation hint. */
  hint?: string;
}

/** A parsed AST node in the kind-tagged astjson shape (see @krate/plugin ASTTypes). */
export type CheckAstNode = { kind: string; [key: string]: unknown };

/** The page surface a custom rule inspects. */
export interface CheckRulePage {
  /** URL route, e.g. "/blog/hello". */
  route: string;
  /** Source file path relative to the project root. */
  source: string;
  /** Final rendered HTML (what ships). */
  html: string;
  /** Client JS weight for this route, in bytes. */
  jsBytes: number;
  /** Parsed program as an astjson document, when available. */
  ast?: CheckAstNode;
}

/** Build context passed to every custom rule. */
export interface CheckRuleContext {
  /** Absolute project root. */
  root: string;
}

/** A custom check rule function. */
export type CheckRuleFn = (
  page: CheckRulePage,
  krate: CheckRuleContext,
) => CheckFinding[];

/**
 * Identity helper that type-checks a custom check rule. Erased at bundle time.
 */
export function defineCheckRule(fn: CheckRuleFn): CheckRuleFn {
  return fn;
}
