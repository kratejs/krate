// Cross-platform command construction for child processes.
//
// On Windows, `npx`, `npm`, `pnpm` and `yarn` are .cmd shims. Node >= 18.20.2 /
// 20.12.2 refuses to spawn a .cmd/.bat without `shell: true` (EINVAL, the
// CVE-2024-27980 fix), and `spawnSync('npm')` without a shell cannot find
// `npm.cmd` at all. With a shell, arguments are joined into one command line
// for cmd.exe, so they have to be quoted here.

export interface CommandSpec {
  command: string;
  args: string[];
  shell: boolean;
}

const SAFE_ARG = /^[A-Za-z0-9_@+=:,./\\-]+$/;

/** Quote one argument for cmd.exe. Throws for characters that cannot be quoted safely. */
export function quoteWindowsArg(arg: string): string {
  if (arg === '') return '""';
  if (/["%\r\n\0]/.test(arg)) {
    throw new Error(
      `Argument ${JSON.stringify(arg)} contains a quote, percent sign or newline, which cannot be passed safely through cmd.exe`,
    );
  }
  if (SAFE_ARG.test(arg)) return arg;
  // Inside double quotes cmd.exe treats & | < > ^ ( ) literally; a trailing
  // backslash would escape the closing quote, so double it.
  return '"' + arg.replace(/\\+$/, (m) => m + m) + '"';
}

/** Build the spawn arguments for running a package-manager style command. */
export function commandSpec(command: string, args: string[], platform: NodeJS.Platform = process.platform): CommandSpec {
  if (platform !== 'win32') return { command, args, shell: false };
  return { command, args: args.map(quoteWindowsArg), shell: true };
}
