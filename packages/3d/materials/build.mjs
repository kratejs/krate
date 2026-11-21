#!/usr/bin/env node
// Compiles materials/src/*.mat into materials/*.filamat with Filament's matc.
// matc comes from KRATE_FILAMENT_MATC or ~/.krate/tools/filament/<version>/bin/matc.
import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, statSync } from 'node:fs';
import { homedir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = dirname(fileURLToPath(import.meta.url));
const version = readFileSync(join(root, 'VERSION'), 'utf8').trim();

// Each source builds an opaque material and a transparent one. The transparent
// build overrides the blending mode and defines KRATE_TRANSPARENT so the shader
// premultiplies by opacity.
const sources = ['standard', 'physical', 'unlit'];
const variants = [
  { suffix: '', args: [] },
  { suffix: '_transparent', args: ['-Pblending=transparent', '-DKRATE_TRANSPARENT=1'] },
];

// matc flags for WebGL 2: OpenGL ES shaders only, size-optimized, without the
// stereo, VSM shadow, fog and screen-space-reflection variants the runtime never enables.
const webFlags = ['-a', 'opengl', '-p', 'mobile', '--optimize-size', '--variant-filter=stereo,vsm,fog,ssr'];

function findMatc() {
  const fromEnv = process.env.KRATE_FILAMENT_MATC;
  if (fromEnv) return fromEnv;
  const exe = process.platform === 'win32' ? 'matc.exe' : 'matc';
  return join(homedir(), '.krate', 'tools', 'filament', version, 'bin', exe);
}

const matc = findMatc();
if (!existsSync(matc)) {
  console.error(`matc not found at ${matc}.`);
  console.error(`Set KRATE_FILAMENT_MATC to a Filament ${version} matc binary, or install one at the path above.`);
  process.exit(1);
}

mkdirSync(root, { recursive: true });
let failed = false;
let total = 0;
for (const name of sources) {
  for (const v of variants) {
    const out = join(root, `${name}${v.suffix}.filamat`);
    const args = [...webFlags, `-Pname=${name}${v.suffix}`, ...v.args, '-o', out, join(root, 'src', `${name}.mat`)];
    const r = spawnSync(matc, args, { stdio: 'inherit' });
    if (r.status !== 0) {
      console.error(`matc failed for ${name}${v.suffix}`);
      failed = true;
      continue;
    }
    const size = statSync(out).size;
    total += size;
    console.log(`${name}${v.suffix}.filamat ${size} bytes`);
  }
}
console.log(`total ${total} bytes (Filament ${version})`);
process.exit(failed ? 1 : 0);
