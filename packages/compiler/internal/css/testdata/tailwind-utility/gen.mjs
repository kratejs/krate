// Generates packages/compiler/internal/css/testdata/tailwind_utility_golden.json from real Tailwind v4.
// Run in a scratch directory that has `npm i tailwindcss @tailwindcss/node`; never inside the repo.
import { compile } from '@tailwindcss/node'
import fs from 'node:fs'
import path from 'node:path'
import { cases } from './cases.mjs'

const out = { tailwind: JSON.parse(fs.readFileSync('node_modules/tailwindcss/package.json', 'utf8')).version, cases: [] }
for (const c of cases) {
  const entry = { name: c.name, css: c.css, classes: {} }
  for (const cls of c.classes) {
    let css = ''
    try {
      const comp = await compile(`@import "tailwindcss/theme.css" layer(theme);\n@tailwind utilities;\n${c.css}`, { base: path.resolve('.'), onDependency() {} })
      css = comp.build([cls])
    } catch (e) { css = 'ERROR: ' + e.message }
    // keep only the utilities layer
    css = css.replace(/\/\*![\s\S]*?\*\/\n?/, '').replace(/@layer theme \{[\s\S]*?\n\}\n/, '').replace(/@property[\s\S]*?\n\}\n?/g, '')
    css = css.replace(/@layer properties \{[\s\S]*?\n\}\n?/g, '').trim()
    entry.classes[cls] = css
  }
  out.cases.push(entry)
}
fs.writeFileSync(process.argv[2] || 'golden.json', JSON.stringify(out, null, 1) + '\n')
