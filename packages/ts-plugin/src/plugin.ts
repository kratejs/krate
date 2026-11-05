import type ts from 'typescript';

// Tier decorators (`@server function Stats() {}`) are compile-time syntax that
// only Krate understands. This plugin removes the errors TypeScript reports for
// them from the editor; `krate typecheck` does the same for tsc, which ignores
// language service plugins. Keep the rules in step with internal/typecheck.

const TIER_LINE = /^[ \t]*@(server|static|runtime|client)(?:[ \t]|\r?$)/;

export function isTierDiagnostic(text: string, start: number, code: number, message: string): boolean {
  const lineStart = text.lastIndexOf('\n', start - 1) + 1;
  const lineEnd = text.indexOf('\n', start);
  const line = text.slice(lineStart, lineEnd < 0 ? text.length : lineEnd);
  const match = TIER_LINE.exec(line);
  if (!match) return false;
  const at = lineStart + line.indexOf('@');
  if (start !== at && start !== at + 1) return false;
  switch (code) {
    case 1206:
    case 1212:
    case 1213:
    case 1214:
      return true;
    case 2304:
      return message.includes(`'${match[1]}'`);
  }
  return false;
}

function keep<T extends ts.Diagnostic>(diagnostics: readonly T[], modules: { typescript: typeof ts }): T[] {
  return diagnostics.filter((d) => {
    if (!d.file || d.start === undefined) return true;
    const message = modules.typescript.flattenDiagnosticMessageText(d.messageText, '\n');
    return !isTierDiagnostic(d.file.text, d.start, d.code, message);
  });
}

export function init(modules: { typescript: typeof ts }): ts.server.PluginModule {
  return {
    create(info) {
      const service = info.languageService;
      const proxy = Object.create(null) as ts.LanguageService;
      for (const key of Object.keys(service) as (keyof ts.LanguageService)[]) {
        const member = service[key];
        (proxy as unknown as Record<string, unknown>)[key] =
          typeof member === 'function' ? (member as (...a: unknown[]) => unknown).bind(service) : member;
      }
      proxy.getSyntacticDiagnostics = (file) => keep(service.getSyntacticDiagnostics(file), modules);
      proxy.getSemanticDiagnostics = (file) => keep(service.getSemanticDiagnostics(file), modules);
      return proxy;
    },
  };
}
