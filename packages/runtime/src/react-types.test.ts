import { describe, expect, it } from 'vitest';
import ts from 'typescript';
import { existsSync } from 'node:fs';
import { resolve } from 'node:path';

// Type-level tests for @krate/runtime/react-types: an app that keeps
// React-typed code must type-check in both JSX modes once the bridge is
// imported, and a plain Krate app (no React types) must be unaffected.

const root = resolve(__dirname, '..');
const jsxTypes = resolve(__dirname, 'jsx-types.ts');
const bridge = resolve(root, 'types/react-types.d.ts');
const stubDir = resolve(root, 'test-fixtures/react-stub');
// Optionally run against the real @types/react: KRATE_REACT_TYPES=/path/to/node_modules
const realTypes = process.env.KRATE_REACT_TYPES;
const virtual = resolve(root, '__react_fixture__.tsx');

type Mode = 'react-jsx' | 'preserve';

function check(source: string, opts: { mode: Mode; react: boolean; bridge: boolean; real?: boolean }): string[] {
  const react = opts.react;
  const typeRoots = react ? [opts.real ? resolve(realTypes!, '@types') : stubDir] : [];
  const options: ts.CompilerOptions = {
    strict: true,
    noEmit: true,
    skipLibCheck: true,
    jsx: opts.mode === 'react-jsx' ? ts.JsxEmit.ReactJSX : ts.JsxEmit.Preserve,
    target: ts.ScriptTarget.ES2022,
    module: ts.ModuleKind.ESNext,
    moduleResolution: ts.ModuleResolutionKind.Bundler,
    lib: ['lib.es2022.d.ts', 'lib.dom.d.ts'],
    types: [],
    typeRoots,
    paths: {
      ...(react && opts.real ? { react: [resolve(realTypes!, '@types/react/index.d.ts')], 'react/jsx-runtime': [resolve(realTypes!, '@types/react/jsx-runtime.d.ts')] } : {}),
      ...(react && !opts.real ? { react: [resolve(stubDir, 'react/index.d.ts')], 'react/jsx-runtime': [resolve(stubDir, 'react/jsx-runtime.d.ts')] } : {}),
      ...(opts.mode === 'preserve' ? { '@krate/runtime/jsx-runtime': [resolve(__dirname, 'jsx-runtime.ts')] } : {}),
    },
    ...(opts.mode === 'preserve' ? { jsxImportSource: '@krate/runtime' } : {}),
  };
  if (react) options.baseUrl = root;
  const host = ts.createCompilerHost(options);
  const readFile = host.readFile.bind(host);
  const fileExists = host.fileExists.bind(host);
  const getSourceFile = host.getSourceFile.bind(host);
  host.fileExists = (f) => f === virtual || fileExists(f);
  host.readFile = (f) => (f === virtual ? source : readFile(f));
  host.getSourceFile = (f, lang, onError, shouldCreate) =>
    f === virtual ? ts.createSourceFile(f, source, lang) : getSourceFile(f, lang, onError, shouldCreate);
  const files = [jsxTypes, virtual, ...(opts.bridge ? [bridge] : [])];
  const program = ts.createProgram(files, options, host);
  return ts
    .getPreEmitDiagnostics(program)
    .filter((d) => !d.file || d.file.fileName === virtual || d.file.fileName === bridge)
    .map((d) => `${d.file ? d.file.fileName.split('/').pop() : ''}: TS${d.code} ${ts.flattenDiagnosticMessageText(d.messageText, '\n')}`);
}

const reactApp = `
import type { ReactNode } from 'react';
function Card(p: { label: ReactNode; children?: ReactNode }) { return <section>{p.label}{p.children}</section>; }
export default function Home() {
  return (
    <Card label={<b>x</b>}>
      <Link href="/">home</Link>
      <div>hi</div>
      <Head><title>t</title></Head>
    </Card>
  );
}
`;

const plainApp = `
export default function Home() {
  return <main><Link href="/">home</Link><Head><title>t</title></Head><div>hi</div></main>;
}
`;

describe('@krate/runtime/react-types', () => {
  it('ships its source declaration file', () => {
    expect(existsSync(bridge)).toBe(true);
    expect(existsSync(resolve(stubDir, 'react/index.d.ts'))).toBe(true);
  });

  const modes: Mode[] = ['react-jsx', 'preserve'];
  for (const mode of modes) {
    describe(`jsx: ${mode}`, () => {
      it('fails without the bridge (the reported bug)', () => {
        const errors = check(reactApp, { mode, react: true, bridge: false });
        expect(errors.length).toBeGreaterThan(0);
        expect(errors.join('\n')).toMatch(mode === 'react-jsx' ? /TS2786/ : /TS2322/);
      });

      it('type-checks with the bridge', () => {
        expect(check(reactApp, { mode, react: true, bridge: true })).toEqual([]);
      });
    });
  }

  it('leaves plain Krate apps (no React types) unchanged', () => {
    expect(check(plainApp, { mode: 'preserve', react: false, bridge: false })).toEqual([]);
  });

  describe.skipIf(!realTypes)('against the real @types/react', () => {
    for (const mode of modes) {
      it(`jsx: ${mode} fails without and passes with the bridge`, () => {
        expect(check(reactApp, { mode, react: true, bridge: false, real: true }).length).toBeGreaterThan(0);
        expect(check(reactApp, { mode, react: true, bridge: true, real: true })).toEqual([]);
      });
    }
  });
});
