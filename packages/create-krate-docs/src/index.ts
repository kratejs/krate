#!/usr/bin/env node
import {
  existsSync,
  mkdirSync,
  readdirSync,
  readFileSync,
  writeFileSync,
} from 'node:fs';
import { basename, dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';
import { createInterface } from 'node:readline/promises';
import { stdin as input, stdout as output } from 'node:process';

const __dirname = dirname(fileURLToPath(import.meta.url));
const packageRoot = resolve(__dirname, '..');
const templatesRoot = join(packageRoot, 'templates');

const pkg = JSON.parse(readFileSync(join(packageRoot, 'package.json'), 'utf-8')) as {
  name: string;
  version: string;
};

const c = {
  reset: '\x1b[0m',
  bold: '\x1b[1m',
  dim: '\x1b[2m',
  red: '\x1b[31m',
  green: '\x1b[32m',
  yellow: '\x1b[33m',
  cyan: '\x1b[36m',
};

type PackageManager = 'npm' | 'pnpm' | 'yarn' | 'bun';

interface Options {
  packageManager: PackageManager;
  skipInstall: boolean;
  git: boolean;
}

const HELP = `
${c.bold}create-krate-docs${c.reset} — scaffold a new Krate documentation site

${c.bold}Usage${c.reset}
  ${c.cyan}create-krate-docs${c.reset} [project-directory] [options]

${c.bold}Options${c.reset}
  --use-npm               Install dependencies with npm
  --use-pnpm              Install dependencies with pnpm
  --use-yarn              Install dependencies with yarn
  --use-bun               Install dependencies with bun
  --skip-install          Skip dependency installation
  --no-git                Do not initialize a git repository
  -h, --help              Show this help message
  -v, --version           Show the CLI version
`;

function help(): void {
  process.stdout.write(HELP);
}

function version(): void {
  process.stdout.write(`${pkg.version}\n`);
}

function fail(message: string): never {
  process.stderr.write(`${c.red}Error:${c.reset} ${message}\n`);
  process.exit(1);
}

function toValidPackageName(name: string): string {
  const cleaned = name
    .trim()
    .toLowerCase()
    .replace(/\s+/g, '-')
    .replace(/[^a-z0-9-_]/g, '')
    .replace(/^[^a-z]+/, '');
  return cleaned || 'krate-docs';
}

function toDisplayName(name: string): string {
  return (
    name
      .split(/[-_ ]+/)
      .filter(Boolean)
      .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
      .join(' ') || 'My Docs'
  );
}

function detectPackageManager(): PackageManager {
  const userAgent = process.env.npm_config_user_agent ?? '';
  if (userAgent.startsWith('pnpm')) return 'pnpm';
  if (userAgent.startsWith('yarn')) return 'yarn';
  if (userAgent.startsWith('bun')) return 'bun';
  return 'npm';
}

function parseArgs(argv: string[]): Partial<Options> & { positional: string[] } {
  const opts: Partial<Options> & { positional: string[] } = { positional: [] };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    switch (arg) {
      case '--use-npm':
        opts.packageManager = 'npm';
        break;
      case '--use-pnpm':
        opts.packageManager = 'pnpm';
        break;
      case '--use-yarn':
        opts.packageManager = 'yarn';
        break;
      case '--use-bun':
        opts.packageManager = 'bun';
        break;
      case '--skip-install':
      case '--no-install':
        opts.skipInstall = true;
        break;
      case '--git':
        opts.git = true;
        break;
      case '--no-git':
        opts.git = false;
        break;
      case '-h':
      case '--help':
        opts.positional.push('__help__');
        break;
      case '-v':
      case '--version':
        opts.positional.push('__version__');
        break;
      default:
        if (arg.startsWith('-')) {
          fail(`Unknown option: ${arg}`);
        }
        opts.positional.push(arg);
        break;
    }
  }
  return opts;
}

async function promptProjectName(): Promise<string> {
  const rl = createInterface({ input, output });
  try {
    const answer = await rl.question(
      `${c.bold}What is your project named?${c.reset} `,
    );
    return answer.trim();
  } finally {
    rl.close();
  }
}

function walkDir(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...walkDir(full));
    else out.push(full);
  }
  return out;
}

function copyTemplate(
  templateDir: string,
  destDir: string,
  vars: Record<string, string>,
): void {
  if (!existsSync(templateDir)) {
    fail(`Template directory not found: ${templateDir}`);
  }
  for (const src of walkDir(templateDir)) {
    const rel = relative(templateDir, src);
    // npm renames `.gitignore` to `.npmignore` when packing, so templates ship
    // a dotless `gitignore` file and we restore the leading dot here.
    const destRel = basename(rel) === 'gitignore' ? join(dirname(rel), '.gitignore') : rel;
    const dest = join(destDir, destRel);
    let content = readFileSync(src, 'utf-8');
    for (const [key, value] of Object.entries(vars)) {
      content = content.split(key).join(value);
    }
    mkdirSync(dirname(dest), { recursive: true });
    writeFileSync(dest, content);
  }
}

function isEmptyDir(dir: string): boolean {
  if (!existsSync(dir)) return true;
  return readdirSync(dir).length === 0;
}

function installDependencies(pm: PackageManager, dir: string): void {
  process.stdout.write(
    `\n${c.bold}Installing dependencies with ${pm}...${c.reset}\n\n`,
  );
  const result = spawnSync(pm, ['install'], { stdio: 'inherit', cwd: dir });
  if (result.status !== 0) {
    fail(`Dependency installation failed. Run \`${pm} install\` manually.`);
  }
}

function initGit(dir: string): void {
  const git = spawnSync('git', ['init', '--quiet'], { cwd: dir });
  if (git.status === 0) {
    spawnSync('git', ['add', '-A'], { cwd: dir });
    spawnSync('git', ['commit', '--quiet', '-m', 'Initial commit'], { cwd: dir });
  }
}

async function main(): Promise<void> {
  const parsed = parseArgs(process.argv.slice(2));

  if (parsed.positional.includes('__help__')) return help();
  if (parsed.positional.includes('__version__')) return version();

  let projectName = parsed.positional[0];
  if (!projectName) {
    projectName = await promptProjectName();
    if (!projectName) fail('A project name is required.');
  }

  const packageManager: PackageManager =
    parsed.packageManager ?? detectPackageManager();
  const skipInstall = parsed.skipInstall ?? false;
  const git = parsed.git ?? true;

  const targetDir = resolve(process.cwd(), projectName);
  const validName = toValidPackageName(basename(projectName));

  if (existsSync(targetDir) && !isEmptyDir(targetDir)) {
    fail(
      `Directory ${targetDir} is not empty. Choose a different name or empty the directory first.`,
    );
  }

  const templateDir = join(templatesRoot, 'docs');

  process.stdout.write(
    `\n${c.bold}Creating a new Krate docs site in ${c.reset}${targetDir}\n\n`,
  );

  copyTemplate(templateDir, targetDir, {
    __PROJECT_NAME__: validName,
    __PROJECT_DISPLAY_NAME__: toDisplayName(projectName),
  });

  const installed = !skipInstall;
  if (installed) installDependencies(packageManager, targetDir);
  if (git) initGit(targetDir);

  const cdDir = projectName === '.' ? basename(targetDir) : projectName;
  const run = packageManager === 'npm' ? 'npm run' : `${packageManager} run`;

  process.stdout.write(`\n${c.bold}${c.green}Success!${c.reset} Created ${cdDir}\n\n`);
  process.stdout.write(`${c.bold}Next steps:${c.reset}\n`);
  process.stdout.write(`  cd ${cdDir}\n`);
  if (!installed) process.stdout.write(`  ${packageManager} install\n`);
  process.stdout.write(`  ${run} dev\n\n`);
}

main().catch((err) => fail(err instanceof Error ? err.message : String(err)));
