// Generates packages/compiler/internal/css/testdata/tailwind_builtin_golden.json from real Tailwind v4.
// Copy this file and cases.mjs into a scratch directory (never inside the repo),
// `npm i tailwindcss @tailwindcss/node` there, then run `node gen.mjs <out.json>`.
import { compile } from '@tailwindcss/node'
import fs from 'node:fs'
import path from 'node:path'
import { fixes, sample, variants } from './cases.mjs'

const base = path.resolve('.')
const themeCss = fs.readFileSync('node_modules/tailwindcss/theme.css', 'utf8')

// Theme variables, so default values can be inlined the way Krate emits them.
const theme = {}
for (const m of themeCss.matchAll(/(--[\w.-]+):\s*([^;]+);/g)) {
  const v = m[2].replace(/\s+/g, ' ').trim()
  if (!v.startsWith('--theme(') && v !== 'initial') theme[m[1]] = v
}
const inline = (css) => {
  for (let i = 0; i < 5; i++) {
    const next = css.replace(/var\((--[\w.-]+)\)/g, (m, n) => (n.startsWith('--tw-') || !(n in theme) ? m : theme[n]))
    if (next === css) break
    css = next
  }
  return css
}
const clean = (css) =>
  css.replace(/\/\*![\s\S]*?\*\/\n?/, '')
    .replace(/@layer theme \{[\s\S]*?\n\}\n/, '')
    .replace(/@property[\s\S]*?\n\}\n?/g, '')
    .replace(/@layer properties \{[\s\S]*?\n\}\n?/g, '')
    .replace(/@layer properties;\n?/g, '')
    .trim()

const input = '@import "tailwindcss/theme.css" layer(theme);\n@tailwind utilities;'
const classes = [...new Set([...fixes, ...sample, ...variants])]
const out = {
  tailwind: JSON.parse(fs.readFileSync('node_modules/tailwindcss/package.json', 'utf8')).version,
  properties: {},
  classes: {},
}
for (const cls of classes) {
  const comp = await compile(input, { base, onDependency() {} })
  out.classes[cls] = inline(clean(comp.build([cls])))
}
// Every @property Tailwind registers for the classes above.
const all = (await compile(input, { base, onDependency() {} })).build(classes)
for (const m of all.matchAll(/@property (--[\w-]+) \{([^}]*)\}/g)) {
  const syntax = m[2].match(/syntax:\s*"([^"]*)"/)[1]
  const initial = m[2].match(/initial-value:\s*([^;]*);/)
  out.properties[m[1]] = { syntax, initial: initial ? initial[1].trim() : null }
}
fs.writeFileSync(process.argv[2] || 'golden.json', JSON.stringify(out, null, 1) + '\n')
