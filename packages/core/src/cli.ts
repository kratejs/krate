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
    console.log('  types     Generate route/content TypeScript declarations');
    console.log('  check     Run compiler-enforced quality gates (a11y/SEO/perf)');
    console.log('  plugin    Manage plugins (e.g. `krate plugin add <pkg>`)');
    console.log('  init      Scaffold a new project (alias: create)');
    console.log('  version   Show version');
    console.log('  mcp       Run the MCP (Model Context Protocol) for model interactions');
    process.exit(0);
  }

  const cmd = args[0];

  // Commands handled by the JS wrapper itself (cross-platform scaffolding) or
  // that need no Go binary. Everything else is forwarded to the native binary so
  // the two command lists cannot drift - a missing entry here was previously
  // swallowing `krate types` and `krate check`.
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
    case 'version':
      console.log(`krate v${version}`);
      break;
    case '--help':
    case '-h':
    case 'help':
      console.log('Usage: krate <command> [options]');
      console.log('');
      console.log('Commands: build, dev, serve, types, check, plugin, init, mcp, version');
      break;
    default:
      try {
        await execBinary(args);
      } catch (err: any) {
        console.error(err.message);
        process.exit(1);
      }
  }
}

main();
