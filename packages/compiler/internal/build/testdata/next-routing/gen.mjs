// Generates golden.json from a real Next 16 build and server.
//
//   node gen.mjs <next-install-root> [out.json]
//
// <next-install-root> is a directory whose node_modules holds next, react, react-dom and typescript (for example a
// pnpm checkout that already installed Next 16). The app is copied into a scratch directory next to this file's
// parent (never into the repo), node_modules is symlinked in, `next build` and `next start` run there, and every
// request in cases.mjs is recorded: status, Location and all response headers (Date and similar are kept; the Go
// test decides which headers may differ, see its allowlist). Re-run by hand when the Next version moves.
import { spawn, spawnSync } from 'node:child_process'
import fs from 'node:fs'
import http from 'node:http'
import net from 'node:net'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { cases } from './cases.mjs'

const here = path.dirname(fileURLToPath(import.meta.url))
const root = path.resolve(process.argv[2] || '')
const out = path.resolve(process.argv[3] || path.join(here, 'golden.json'))
const scratch = process.env.NEXT_ROUTING_SCRATCH || fs.mkdtempSync(path.join(process.env.HOME || os.homedir(), 'wt', 'next-routing-gen-'))
const work = path.join(scratch, 'app')
fs.rmSync(work, { recursive: true, force: true })
fs.cpSync(path.join(here, 'app'), work, { recursive: true })
fs.mkdirSync(path.join(work, 'node_modules', '@types'), { recursive: true })
for (const name of ['next', 'react', 'react-dom', 'typescript']) {
  fs.symlinkSync(fs.realpathSync(path.join(root, 'node_modules', name)), path.join(work, 'node_modules', name))
}
for (const name of ['node', 'react', 'react-dom']) {
  const p = path.join(root, 'node_modules', '@types', name)
  if (fs.existsSync(p)) fs.symlinkSync(fs.realpathSync(p), path.join(work, 'node_modules', '@types', name))
}
const nextBin = path.join(work, 'node_modules', 'next', 'dist', 'bin', 'next')
const version = JSON.parse(fs.readFileSync(path.join(work, 'node_modules', 'next', 'package.json'), 'utf8')).version
const marker = path.join(scratch, 'instrumentation.marker')
fs.rmSync(marker, { force: true })
const env = { ...process.env, NEXT_TELEMETRY_DISABLED: '1', NODE_ENV: 'production' }
delete env.INSTR_MARKER
// Next refuses to build a version with a known advisory when it detects a coding agent in the environment (it wants
// to run its own upgrade first). The golden records the routing of the pinned version, so hide the agent variables.
for (const name of ['AI_AGENT', 'CLAUDECODE', 'CLAUDE_CODE', 'CURSOR_AGENT', 'CODEX_SANDBOX', 'CODEX_CI', 'GEMINI_CLI', 'OPENCODE_CLIENT']) delete env[name]

const build = spawnSync(process.execPath, [nextBin, 'build', '--webpack'], { cwd: work, env, stdio: 'inherit' })
if (build.status !== 0) throw new Error('next build failed')

const port = await new Promise((resolve) => {
  const s = net.createServer().listen(0, '127.0.0.1', () => {
    const p = s.address().port
    s.close(() => resolve(p))
  })
})
const server = spawn(process.execPath, [nextBin, 'start', '-H', '127.0.0.1', '-p', String(port)], {
  cwd: work,
  env: { ...env, INSTR_MARKER: marker },
  stdio: ['ignore', 'inherit', 'inherit'],
})
try {
  for (let i = 0; ; i++) {
    try {
      await request({ path: '/api/marker' })
      break
    } catch (err) {
      if (i > 100) throw err
      await new Promise((r) => setTimeout(r, 200))
    }
  }
  const results = []
  for (const c of cases) results.push({ ...c, response: await request(c) })
  const registers = fs.existsSync(marker) ? fs.readFileSync(marker, 'utf8').split('\n').filter(Boolean).length : 0
  fs.writeFileSync(out, JSON.stringify({ next: version, registers, cases: results }, null, 1) + '\n')
  console.log(`wrote ${out}: ${results.length} cases, next ${version}, register() ran ${registers} time(s)`)
} finally {
  server.kill()
}

function request(c) {
  return new Promise((resolve, reject) => {
    const qs = c.query ? '?' + new URLSearchParams(c.query).toString() : ''
    const headers = { ...(c.headers || {}) }
    if (c.cookies) headers.cookie = Object.entries(c.cookies).map(([k, v]) => `${k}=${v}`).join('; ')
    const req = http.request({ host: '127.0.0.1', port, method: c.method || 'GET', path: c.path + qs, headers }, (res) => {
      const chunks = []
      res.on('data', (d) => chunks.push(d))
      res.on('end', () => {
        const h = {}
        for (const [k, v] of Object.entries(res.headers)) if (!['date', 'connection', 'keep-alive'].includes(k)) h[k] = Array.isArray(v) ? v.join(', ') : v
        resolve({ status: res.statusCode, location: res.headers.location || null, headers: h, body: Buffer.concat(chunks).toString('utf8').slice(0, 300) })
      })
    })
    req.on('error', reject)
    req.end()
  })
}
