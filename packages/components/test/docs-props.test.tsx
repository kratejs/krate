import { describe, it, expect } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

// Keeps the Props column of the component library reference in step with the
// TypeScript interfaces: every documented prop must exist, and every prop an
// interface declares must be documented.
const root = join(__dirname, '..');
const docPath = join(root, '..', 'web', 'src', 'content', 'docs', 'reference', 'component-library.md');

function interfaceProps(): Record<string, string[]> {
  const out: Record<string, string[]> = {};
  const srcDir = join(root, 'src');
  for (const dir of readdirSync(srcDir, { withFileTypes: true })) {
    if (!dir.isDirectory()) continue;
    for (const file of readdirSync(join(srcDir, dir.name))) {
      if (!file.endsWith('.tsx')) continue;
      const text = readFileSync(join(srcDir, dir.name, file), 'utf8');
      for (const m of text.matchAll(/export interface (\w+)Props \{([\s\S]*?)\n\}/g)) {
        out[m[1]] = [...m[2].matchAll(/^ {2}(\w+)\??:/gm)].map((x) => x[1]).filter((n) => n !== 'children');
      }
    }
  }
  return out;
}

function documented(): Array<{ names: string[]; props: string[]; cell: string }> {
  const rows: Array<{ names: string[]; props: string[]; cell: string }> = [];
  let inProps = false;
  for (const line of readFileSync(docPath, 'utf8').split('\n')) {
    if (line.startsWith('| Component | Description | Props |')) inProps = true;
    else if (!line.startsWith('|')) inProps = false;
    if (!inProps) continue;
    const m = line.match(/^\| ((?:`\w+`(?: \/ )?)+) \| .* \| (.*) \|$/);
    if (!m) continue;
    const names = [...m[1].matchAll(/`(\w+)`/g)].map((x) => x[1]);
    rows.push({ names, props: [...m[2].matchAll(/`(\w+)`/g)].map((x) => x[1]), cell: m[2] });
  }
  return rows;
}

describe('component library reference', () => {
  const declared = interfaceProps();
  const rows = documented();

  it('documents a Props column for every component row', () => {
    expect(rows.length).toBeGreaterThan(40);
  });

  it('lists exactly the props each component declares', () => {
    for (const row of rows) {
      if (row.names.length > 1) {
        // "Select / NativeSelect" style rows: each name prefixed in the cell.
        for (const name of row.names) {
          const part = row.cell.split(';').find((p) => p.trim().startsWith(name + ':'));
          const got = part ? [...part.matchAll(/`(\w+)`/g)].map((x) => x[1]) : [];
          expect(got.sort(), name).toEqual([...(declared[name] || [])].sort());
        }
      } else {
        const name = row.names[0];
        expect(row.props.sort(), name).toEqual([...(declared[name] || [])].sort());
      }
    }
  });
});
