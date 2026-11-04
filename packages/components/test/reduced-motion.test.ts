import { describe, it, expect } from 'vitest';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

const SRC = join(__dirname, '..', 'src');

function cssFiles(dir: string): string[] {
  const out: string[] = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) out.push(...cssFiles(p));
    else if (name.endsWith('.css')) out.push(p);
  }
  return out;
}

interface Rule {
  selectors: string[];
  body: string;
  inReduced: boolean;
}

// Minimal CSS walker: returns style rules with their selector lists, noting
// whether they sit inside a prefers-reduced-motion block.
function rules(css: string): Rule[] {
  const src = css.replace(/\/\*[\s\S]*?\*\//g, '');
  const out: Rule[] = [];
  const walk = (text: string, reduced: boolean): void => {
    let i = 0;
    while (i < text.length) {
      const open = text.indexOf('{', i);
      if (open < 0) break;
      const prelude = text.slice(i, open).trim();
      let depth = 1;
      let j = open + 1;
      while (j < text.length && depth > 0) {
        if (text[j] === '{') depth++;
        else if (text[j] === '}') depth--;
        j++;
      }
      const body = text.slice(open + 1, j - 1);
      if (prelude.startsWith('@media')) walk(body, reduced || /prefers-reduced-motion:\s*reduce/.test(prelude));
      else if (!prelude.startsWith('@')) {
        out.push({ selectors: prelude.split(',').map((s) => s.trim()), body, inReduced: reduced });
      }
      i = j;
    }
  };
  walk(src, false);
  return out;
}

const motion = /(^|[;\s])(transition|animation)(-[a-z-]+)?\s*:/;

describe('reduced motion coverage', () => {
  const tokens = rules(readFileSync(join(SRC, 'tokens.css'), 'utf8')).filter((r) => r.inReduced);
  const covered = new Set(tokens.flatMap((r) => r.selectors));

  it('tokens.css disables transitions and animations for every krate class', () => {
    expect(covered.has('[class*="krate-"]')).toBe(true);
    const main = tokens.find((r) => r.selectors.includes('[class*="krate-"]'))!;
    expect(main.body).toMatch(/transition-duration:\s*0s/);
    expect(main.body).toMatch(/animation-duration:\s*0\.01ms/);
    expect(main.body).toMatch(/animation-iteration-count:\s*1/);
  });

  it('covers every selector with a transition or animation', () => {
    const uncovered: string[] = [];
    let seen = 0;
    for (const file of cssFiles(SRC)) {
      if (file.endsWith('tokens.css')) continue;
      for (const r of rules(readFileSync(file, 'utf8'))) {
        if (r.inReduced || !motion.test(r.body)) continue;
        for (const sel of r.selectors) {
          seen++;
          // Subject compound: text after the last combinator.
          const subject = sel.split(/\s*[>+~]\s*|\s+/).pop()!;
          const pseudo = /(::[a-z-]+)$/.exec(subject)?.[1] ?? '';
          const classes = subject.replace(/(::[a-z-]+)$/, '').match(/\.[A-Za-z0-9_-]+/g) ?? [];
          const hasKrate = classes.some((c) => c.startsWith('.krate-'));
          const okPseudo =
            pseudo === '' ||
            ((pseudo === '::before' || pseudo === '::after') && covered.has(`[class*="krate-"]${pseudo}`)) ||
            covered.has(subject);
          if (!hasKrate || !okPseudo) uncovered.push(`${file}: ${sel}`);
        }
      }
    }
    expect(seen).toBeGreaterThan(20);
    expect(uncovered).toEqual([]);
  });
});
