import { readFileSync } from 'fs';
import { join, dirname } from 'path';
import { fileURLToPath } from 'url';
import { spawn } from 'child_process';
import { execBinary, printVersion } from './binary.js';
import { commandSpec } from './spawn.js';

const __dirname = dirname(fileURLToPath(import.meta.url));
const version = JSON.parse(
  readFileSync(join(__dirname, '..', 'package.json'), 'utf-8'),
).version;

// scaffold delegates `krate init` / `krate create` to create-krate-app, the
// official scaffold CLI. It lives in the JS wrapper (not the Go binary) so it
// works on every platform and follows the core package's release channel.
function scaffold(args: string[]): Promise<void> {
  return new Promise((resolve, reject) => {
    const channel = /^[^-]+-([A-Za-z]+)/.exec(version);
    const npxArgs = ['--yes', 'create-krate-app@' + (channel ? channel[1] : 'latest'), ...args.slice(1)];

    const spec = commandSpec('npx', npxArgs);
    const proc = spawn(spec.command, spec.args, { stdio: 'inherit', shell: spec.shell });
    proc.on('close', (code) => {
      if (code === 0) resolve();
      else {
        const err = new Error(`create-krate-app exited with code ${code}`) as Error & { exitCode?: number };
        err.exitCode = code ?? 1;
        reject(err);
      }
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
    console.log('  typecheck Generate types and run tsc');
    console.log('  check     Run compiler-enforced quality gates (a11y/SEO/perf)');
    console.log('  fix       Apply safe source fixes');
    console.log('  migrate   Plan or apply a framework migration');
    console.log('  inspect   Print the compiled site model');
    console.log('  deploy    Build and write host adapter files');
    console.log('  clean     Remove build output and caches');
    console.log('  doctor    Print a diagnostic summary');
    console.log('  config    Print the resolved configuration');
    console.log('  go:init   Set up Go module helpers');
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
        process.exit(err.exitCode || 1);
      }
      break;
    case 'version':
      try {
        await printVersion(args, version);
      } catch (err: any) {
        console.error(err.message);
        process.exit(1);
      }
      break;
    case '--help':
    case '-h':
    case 'help':
      // Forward help so command-specific help and the canonical command list
      // come from the native binary instead of drifting here.
      try {
        await execBinary(args, undefined, version);
      } catch (err: any) {
        console.error(err.message);
        process.exit(err.exitCode || 1);
      }
      break;
    default:
      try {
        await execBinary(args, undefined, version);
      } catch (err: any) {
        console.error(err.message);
        process.exit(err.exitCode || 1);
      }
  }
}

main();
