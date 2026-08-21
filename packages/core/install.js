import { existsSync, mkdirSync, readFileSync } from 'fs';
import { join, dirname } from 'path';
import { fileURLToPath } from 'url';
import { createRequire } from 'module';

const __dirname = dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);

const binDir = join(__dirname, 'bin');
const platform = process.platform;
const arch = process.arch;
const targetKey = `${platform}-${arch}`;
const binaryName = platform === 'win32' ? 'krate.exe' : 'krate';

function resolveBinary() {
  try {
    const pkgName = `@krate/core-${targetKey}`;
    const pkgPath = dirname(require.resolve(`${pkgName}/package.json`));
    const binary = join(pkgPath, 'bin', binaryName);
    if (existsSync(binary)) return binary;
  } catch {
    // Platform package not installed — fall through to local binary.
  }
  const local = join(binDir, binaryName);
  if (existsSync(local)) return local;
  return null;
}

const binary = resolveBinary();
if (!binary) {
  console.log(
    `No krate binary found for ${targetKey}. To build from source: ` +
      `cd packages/compiler && go build -o ../core/bin/${binaryName} ./cmd/krate`,
  );
} else {
  console.log('krate installed successfully!');
}
