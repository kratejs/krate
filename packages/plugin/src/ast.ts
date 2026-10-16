/**
 * AST types for Krate JS plugins.
 *
 * The `AfterParse` hook receives `ctx.program` as a kind-tagged AST document:
 * every node is a plain object with a `kind` discriminator plus its fields
 * (see `internal/astjson`). Prefer {@link ASTTypes} over bare string literals
 * for the discriminator so comparisons are typo-proof and autocompleted:
 *
 * ```ts
 * import { ASTTypes, isAstKind } from "@krate/plugin";
 *
 * if (node.kind === ASTTypes.JSXText && node.value === "GO-PLUGIN") { ... }
 * if (isAstKind(node, ASTTypes.JSXElement)) { ... }
 * ```
 *
 * `ASTTypes` is a frozen const object (value + type) that mirrors the Go node
 * registry one-to-one. `AstKind` is the union of its values. The Go registry
 * (`internal/astjson/astjson.go` `nodeTypes`) is the source of truth; a Go test
 * asserts this file stays in sync with it.
 */

/** Discriminator values for every node in the kind-tagged AST document. */
export const ASTTypes = Object.freeze({
  // Expressions
  Identifier: "Identifier",
  Literal: "Literal",
  CallExpr: "CallExpr",
  MemberExpr: "MemberExpr",
  BinaryExpr: "BinaryExpr",
  UnaryExpr: "UnaryExpr",
  ConditionalExpr: "ConditionalExpr",
  TypeAssertion: "TypeAssertion",
  ArrowFn: "ArrowFn",
  ObjectExpr: "ObjectExpr",
  ObjectProp: "ObjectProp",
  ArrayExpr: "ArrayExpr",
  TemplateExpr: "TemplateExpr",
  // JSX
  JSXElement: "JSXElement",
  JSXFragment: "JSXFragment",
  JSXOpening: "JSXOpening",
  JSXClosing: "JSXClosing",
  JSXText: "JSXText",
  JSXExprContainer: "JSXExprContainer",
  JSXElementChild: "JSXElementChild",
  JSXFragmentChild: "JSXFragmentChild",
  JSXAttr: "JSXAttr",
  // Declarations + statements
  Param: "Param",
  Program: "Program",
  ImportStmt: "ImportStmt",
  NamedImport: "NamedImport",
  ExportStmt: "ExportStmt",
  VarStmt: "VarStmt",
  VarDecl: "VarDecl",
  FnDecl: "FnDecl",
  ReturnStmt: "ReturnStmt",
  ExprStmt: "ExprStmt",
  IfStmt: "IfStmt",
  BlockStmt: "BlockStmt",
  ForStmt: "ForStmt",
  ForInStmt: "ForInStmt",
  WhileStmt: "WhileStmt",
  DoWhileStmt: "DoWhileStmt",
  SwitchStmt: "SwitchStmt",
  CaseClause: "CaseClause",
  TryStmt: "TryStmt",
  CatchClause: "CatchClause",
  ThrowStmt: "ThrowStmt",
  BreakStmt: "BreakStmt",
  ContinueStmt: "ContinueStmt",
  NewExpr: "NewExpr",
  ThisExpr: "ThisExpr",
  AwaitExpr: "AwaitExpr",
  DynamicImport: "DynamicImport",
  ImportMetaExpr: "ImportMetaExpr",
} as const);

/** Union of every AST node `kind` discriminator. */
export type AstKind = (typeof ASTTypes)[keyof typeof ASTTypes];

/**
 * `Literal.literalKind` values. The field is renamed from `Kind` in the encoded
 * document (to avoid clashing with the `kind` discriminator) and carries the
 * numeric Go `ast.LitKind` enum.
 */
export const ASTLiteralKinds = Object.freeze({
  String: 0,
  Number: 1,
  Bool: 2,
  Null: 3,
  Regexp: 4,
} as const);

export type AstLiteralKind =
  (typeof ASTLiteralKinds)[keyof typeof ASTLiteralKinds];

/**
 * `VarStmt.variableKind` values. Renamed from `Kind`; carries the numeric Go
 * `ast.VarKind` enum.
 */
export const ASTVarKinds = Object.freeze({
  Const: 0,
  Let: 1,
  Var: 2,
} as const);

export type AstVarKind = (typeof ASTVarKinds)[keyof typeof ASTVarKinds];

/** A position span (`line`/`col`, 1-based as produced by the lexer). */
export interface AstPosition {
  line: number;
  col: number;
}

/**
 * The base shape every AST node shares. Concrete nodes carry additional fields;
 * use a `kind` guard (or {@link isAstKind}) to narrow.
 */
export interface AstNode {
  kind: AstKind;
  position?: AstPosition;
  [field: string]: unknown;
}

/** Narrowing helper: a node of a specific `kind` (fields still `unknown`). */
export type AstNodeOf<K extends AstKind> = AstNode & { kind: K };

/** Type guard: narrows `value` to a node with the given `kind`. */
export function isAstKind<K extends AstKind>(
  value: unknown,
  kind: K,
): value is AstNodeOf<K> {
  return (
    typeof value === "object" &&
    value !== null &&
    (value as { kind?: unknown }).kind === kind
  );
}

/** All valid `kind` values, handy for validation or docs generation. */
export const AST_KINDS: readonly AstKind[] = Object.freeze(
  Object.values(ASTTypes),
);
