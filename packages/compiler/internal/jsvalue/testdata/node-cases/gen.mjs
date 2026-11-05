// Regenerates differential_node_data_test.go and testdata/date_locale_golden.json
// from Node. Run with the time zone the build evaluates in:
//
//   TZ=UTC node gen.mjs <out dir>
//
// where <out dir> is packages/compiler/internal/jsvalue.
import fs from 'node:fs'
import path from 'node:path'
import { exprs } from './cases.mjs'

const outDir = process.argv[2] || '.'
const goQ = (s) => JSON.stringify(s).replace(/[\u0080-￿]/g, (c) => '\\u' + c.charCodeAt(0).toString(16).padStart(4, '0'))

let go = `package jsvalue

// extraNodeCases extend TestInterpreterNeverDisagreesWithNode with iterators,
// Map, Set and Date. Expected values are String(value) from Node ${process.version} run with
// TZ=UTC (the time zone the build evaluates in) and the default en-US locale;
// regenerate them with testdata/node-cases/gen.mjs.
var extraNodeCases = []struct{ src, want string }{
`
for (const e of exprs) {
  let want
  try { want = String(eval(e)) } catch (err) { continue }
  go += `\t{${goQ(e)}, ${goQ(want)}},\n`
}
go += '}\n'
fs.writeFileSync(path.join(outDir, 'differential_node_data_test.go'), go)

// Locale matrix: toLocale*String with en-US options, over a few instants.
const instants = [
  'Date.UTC(2026, 10, 5, 14, 37, 15, 123)',
  'Date.UTC(2026, 4, 3, 0, 7, 5, 9)',
  'Date.UTC(2020, 11, 31, 23, 59, 59, 999)',
  'Date.UTC(2021, 0, 1, 12, 0, 0, 0)',
]
const cases = []
// A case is [instant index, method, locales JSON or "", options JSON or "", String(result)].
const add = (method, locale, opts, ts) => {
  const o = opts === undefined ? '' : JSON.stringify(opts)
  const l = locale === undefined ? '' : JSON.stringify(locale)
  const args = [l || (o ? 'undefined' : ''), o].filter(Boolean).join(', ')
  const src = `new Date(${instants[ts]}).${method}(${args})`
  let want
  try { want = String(eval(src)) } catch (e) { want = 'THROWS ' + e.constructor.name }
  cases.push([ts, method, l, o, want])
}
const pick = (arr, key) => arr.map((v) => (v === null ? {} : { [key]: v }))
const combos = (spec) => spec.reduce((acc, [key, vals]) => acc.flatMap((o) => vals.map((v) => (v === null ? o : { ...o, [key]: v }))), [{}])

for (const ts of [0, 1]) {
  for (const m of ['toLocaleDateString', 'toLocaleTimeString', 'toLocaleString']) {
    add(m, undefined, undefined, ts)
    add(m, 'en-US', undefined, ts)
    add(m, 'en-US', {}, ts)
    add(m, 'en', undefined, ts)
    add(m, ['en-US'], undefined, ts)
  }
}
// Date fields
for (const ts of [0, 1, 2]) {
  for (const o of combos([['weekday', [null, 'short', 'long', 'narrow']], ['year', [null, 'numeric', '2-digit']], ['month', [null, 'numeric', '2-digit', 'short', 'long', 'narrow']], ['day', [null, 'numeric', '2-digit']]])) {
    add('toLocaleDateString', 'en-US', o, ts)
  }
}
// Time fields
for (const ts of [0, 1]) {
  for (const o of combos([['hour', [null, 'numeric', '2-digit']], ['minute', [null, 'numeric', '2-digit']], ['second', [null, 'numeric', '2-digit']], ['hour12', [null, true, false]], ['timeZoneName', [null, 'short', 'long']]])) {
    add('toLocaleTimeString', 'en-US', o, ts)
  }
}
// Date and time together
const dateSets = [{}, { year: 'numeric', month: 'numeric', day: 'numeric' }, { year: 'numeric', month: 'short', day: 'numeric' }, { year: 'numeric', month: 'long', day: 'numeric' }, { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' }, { month: 'long', day: 'numeric' }, { weekday: 'short', month: 'short', day: 'numeric' }, { year: 'numeric' }, { month: 'long' }]
const timeSets = [{}, { hour: 'numeric' }, { hour: 'numeric', minute: '2-digit' }, { hour: '2-digit', minute: '2-digit', second: '2-digit' }, { hour: 'numeric', minute: 'numeric', hour12: false }, { hour: 'numeric', minute: 'numeric', timeZoneName: 'short' }]
for (const ts of [0, 1]) {
  for (const d of dateSets) for (const t of timeSets) {
    add('toLocaleString', 'en-US', { ...d, ...t }, ts)
    add('toLocaleString', 'en-US', { ...d, ...t, timeZone: 'UTC' }, ts)
    if (ts === 0) {
      add('toLocaleDateString', 'en-US', { ...d, ...t }, ts)
      add('toLocaleTimeString', 'en-US', { ...d, ...t }, ts)
    }
  }
}
// Styles
const styles = [undefined, 'full', 'long', 'medium', 'short']
for (const ts of [0, 1]) {
  for (const ds of styles) for (const tsty of styles) {
    if (!ds && !tsty) continue
    const o = {}
    if (ds) o.dateStyle = ds
    if (tsty) o.timeStyle = tsty
    add('toLocaleString', 'en-US', o, ts)
    add('toLocaleDateString', 'en-US', o, ts)
    add('toLocaleTimeString', 'en-US', o, ts)
    if (tsty) add('toLocaleString', 'en-US', { ...o, hour12: false }, ts)
  }
}
// Options and locales the evaluator must refuse rather than guess.
for (const ts of [0]) {
  for (const loc of ['de-DE', 'en-GB', 'fr', 'ja-JP']) add('toLocaleDateString', loc, undefined, ts)
  for (const tz of ['Asia/Tokyo', 'America/New_York']) add('toLocaleString', 'en-US', { timeZone: tz }, ts)
  add('toLocaleDateString', 'en-US', { era: 'short' }, ts)
  add('toLocaleDateString', 'en-US', { month: 'bogus' }, ts)
  add('toLocaleTimeString', 'en-US', { hourCycle: 'h11' }, ts)
  add('toLocaleTimeString', 'en-US', { fractionalSecondDigits: 3 }, ts)
  add('toLocaleTimeString', 'en-US', { dayPeriod: 'long' }, ts)
}
fs.writeFileSync(path.join(outDir, 'testdata', 'date_locale_golden.json'), JSON.stringify({ node: process.version, instants, cases }).replace(/\],\[/g, '],\n[') + '\n')
console.log(cases.length, 'locale cases')
