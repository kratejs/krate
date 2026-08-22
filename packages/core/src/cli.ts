import { readFileSync } from 'fs';
import { join, dirname } from 'path';
import { fileURLToPath } from 'url';
import { spawn } from 'child_process';
import { execBinary } from './binary.js';

const __dirname = dirname(fileURLToPath(import.meta.url));
const version = JSON.parse(
  readFileSync(join(__dirname, '..', 'package.json'), 'utf-8'),
).version;

// scaffold delegates `krate init` / `krate create` to create-krate-app, the
// official scaffold CLI. It lives in the JS wrapper (not the Go binary) so it
// works on every platform and always pulls the latest published scaffold.
function scaffold(args: string[]): Promise<void> {
  return new Promise((resolve, reject) => {
    const npxCmd = process.platform === 'win32' ? 'npx.cmd' : 'npx';
    const npxArgs = ['--yes', 'create-krate-app@latest'];
    const dir = args[1];
    if (dir) npxArgs.push(dir);

    const proc = spawn(npxCmd, npxArgs, { stdio: 'inherit' });
    proc.on('close', (code) => {
      if (code === 0) resolve();
      else reject(new Error(`create-krate-app exited with code ${code}`));
    });
    proc.on('error', (err) => {
      reject(new Error(`Failed to run create-krate-app: ${err.message}`));
    });
  });
}

async function main() {
  const args = process.argv.slice(2);

  if (args.length === 0) {
    console.log('Usage: krate <command> [options]');
    console.log('');
    console.log('Commands:');
    console.log('  build     Build project for production');
    console.log('  dev       Start development server');
    console.log('  serve     Build and serve for preview');
    console.log('  init      Scaffold a new project (alias: create)');
    console.log('  version   Show version');
    process.exit(0);
  }

  const cmd = args[0];

  switch (cmd) {
    case 'init':
    case 'create':
      try {
        await scaffold(args);
      } catch (err: any) {
        console.error(err.message);
        process.exit(1);
      }
      break;
    case 'build':
    case 'dev':
    case 'serve':
      try {
        await execBinary(args);
      } catch (err: any) {
        console.error(err.message);
        process.exit(1);
      }
      break;
    case 'version':
      console.log(`krate v${version}`);
      break;
    default:
      console.error(`Unknown command: ${cmd}`);
      process.exit(1);
  }
}

main();
