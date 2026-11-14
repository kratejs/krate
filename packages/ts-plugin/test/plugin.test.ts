import { describe, expect, it } from 'vitest';
import ts from 'typescript';
import { init } from '../src/plugin';

const FILE = '/app/a.tsx';

function service(source: string): ts.LanguageService {
  const options: ts.CompilerOptions = {
    target: ts.ScriptTarget.ES2022,
    module: ts.ModuleKind.ESNext,
    moduleResolution: ts.ModuleResolutionKind.Bundler,
    jsx: ts.JsxEmit.Preserve,
    strict: true,
    noLib: true,
    types: [],
  };
  const host: ts.LanguageServiceHost = {
    getCompilationSettings: () => options,
    getScriptFileNames: () => [FILE],
    getScriptVersion: () => '1',
    getScriptSnapshot: (name) => (name === FILE ? ts.ScriptSnapshot.fromString(source) : undefined),
    getCurrentDirectory: () => '/app',
    getDefaultLibFileName: () => '/lib.d.ts',
    fileExists: (name) => name === FILE,
    readFile: (name) => (name === FILE ? source : undefined),
  };
  return ts.createLanguageService(host);
}

function codes(ls: ts.LanguageService): number[] {
  return [...ls.getSyntacticDiagnostics(FILE), ...ls.getSemanticDiagnostics(FILE)]
    .map((d) => d.code)
    .filter((c) => c !== 2318 && c !== 2552)
    .sort();
}

function withPlugin(ls: ts.LanguageService): ts.LanguageService {
  const created = init({ typescript: ts });
  return created.create({ languageService: ls } as unknown as ts.server.PluginCreateInfo);
}

describe('language service plugin', () => {
  const source = `@server
export function A() { return null; }
@static
export function B() { return null; }
@client
export const C = () => null;
@runtime
export default class D {}
`;

  it('removes them all', () => {
    expect(codes(withPlugin(service(source)))).toEqual([]);
  });

  it('keeps a misspelled decorator and real errors', () => {
    const ls = withPlugin(service('@sever\nexport class E {}\nexport const z: string = null;\n'));
    expect(codes(ls)).toEqual(expect.arrayContaining([2304, 2322]));
  });
});
