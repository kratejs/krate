import { describe, expect, it } from 'vitest';
import ts from 'typescript';
import { resolve } from 'node:path';

const typesFile = resolve(__dirname, 'jsx-types.ts');
const virtual = resolve(__dirname, '__svg_fixture__.tsx');

function check(source: string): string[] {
  const options: ts.CompilerOptions = {
    strict: true,
    noEmit: true,
    skipLibCheck: true,
    jsx: ts.JsxEmit.Preserve,
    target: ts.ScriptTarget.ES2020,
    module: ts.ModuleKind.ESNext,
    moduleResolution: ts.ModuleResolutionKind.Bundler,
    types: [],
  };
  const host = ts.createCompilerHost(options);
  const readFile = host.readFile.bind(host);
  const fileExists = host.fileExists.bind(host);
  const getSourceFile = host.getSourceFile.bind(host);
  host.fileExists = (f) => f === virtual || fileExists(f);
  host.readFile = (f) => (f === virtual ? source : readFile(f));
  host.getSourceFile = (f, lang, onError, shouldCreate) =>
    f === virtual ? ts.createSourceFile(f, source, lang) : getSourceFile(f, lang, onError, shouldCreate);
  const program = ts.createProgram([typesFile, virtual], options, host);
  return ts
    .getPreEmitDiagnostics(program)
    .filter((d) => d.file?.fileName === virtual)
    .map((d) => ts.flattenDiagnosticMessageText(d.messageText, '\n'));
}

describe('SVG JSX types', () => {
  it('accepts a pasted Figma/Illustrator export', () => {
    const errors = check(`
export const Pasted = () => (
  <svg xmlns="http://www.w3.org/2000/svg" xmlnsXlink="http://www.w3.org/1999/xlink" xmlns:xlink="http://www.w3.org/1999/xlink"
    xmlSpace="preserve" xml:space="preserve" version="1.1" width="24" height="24" viewBox="0 0 24 24" fill="none" onClick={() => {}}>
    <defs>
      <linearGradient id="g" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" stopColor="#000" />
      </linearGradient>
      <clipPath id="c"><rect width="24" height="24" /></clipPath>
      <filter id="f"><feColorMatrix type="matrix" values="1 0 0 0 0" /><feGaussianBlur stdDeviation="2" /></filter>
    </defs>
    <g clipPath="url(#c)" filter="url(#f)">
      <use xlinkHref="#g" xlink:href="#g" />
      <ellipse cx="1" cy="1" rx="1" ry="1" />
      <text x="0" y="0"><tspan>hi</tspan></text>
      <path fillRule="evenodd" clipRule="evenodd" d="M0 0h24v24H0z" strokeLinecap="round" />
    </g>
  </svg>
);
`);
    expect(errors).toEqual([]);
  });

  it('still rejects unknown SVG attributes', () => {
    const errors = check(`export const Bad = () => <svg notAnAttribute="x" />;`);
    expect(errors.length).toBeGreaterThan(0);
  });
});
