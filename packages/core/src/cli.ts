import { readFileSync } from 'fs';
import { join, dirname } from 'path';
import { fileURLToPath } from 'url';
import { execBinary } from './binary.js';

const __dirname = dirname(fileURLToPath(import.meta.url));
const version = JSON.parse(
  readFileSync(join(__dirname, '..', 'package.json'), 'utf-8'),
).version;

async function main() {
  const args = process.argv.slice(2);

  if (args.length === 0) {
    console.log('Usage: krate <command> [options]');
    console.log('');
    console.log('Commands:');
    console.log('  build     Build project for production');
    console.log('  dev       Start development server');
    console.log('  init      Scaffold a new project');
    console.log('  version   Show version');
    process.exit(0);
  }

  const cmd = args[0];

  switch (cmd) {
    case 'build':
    case 'dev':
    case 'serve':
    case 'init':
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
