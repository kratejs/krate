/**
 * Pure helpers for smoke-install.mjs, split out so node --test can cover them
 * without a Windows or Alpine machine.
 */

const SAFE_ARG = /^[A-Za-z0-9_@+=:,./\\-]+$/;

/** Quote one argument for cmd.exe, the shell npm.cmd and pnpm.cmd need. */
export function quoteCmdArg(arg) {
  if (arg === '') return '""';
  if (/["%\r\n\0]/.test(arg)) {
    throw new Error(`argument ${JSON.stringify(arg)} has a quote, percent sign or newline, which cmd.exe cannot take safely`);
  }
  if (SAFE_ARG.test(arg)) return arg;
  // A trailing backslash would escape the closing quote.
  return `"${arg.replace(/\\+$/, (m) => m + m)}"`;
}

/**
 * How to start a command. Package managers are .cmd shims on Windows: they
 * need a shell, and a shell joins the arguments unquoted, so paths with spaces
 * would split. Compiled binaries (.exe) are started directly, never through a
 * shell, so their paths are passed as they are.
 */
export function spawnPlan(command, args, platform = process.platform) {
  const needsShell = platform === 'win32' && !/\.exe$/i.test(command);
  return {
    command,
    args: needsShell ? args.map(quoteCmdArg) : args,
    shell: needsShell,
  };
}

/** True on a Linux without glibc (Alpine). Mirrors isMusl() in packages/core/src/binary.ts. */
export function isMusl(report = process.report, platform = process.platform) {
  if (platform !== 'linux') return false;
  try {
    const header = report?.getReport?.().header;
    return !!header && !header.glibcVersionRuntime;
  } catch {
    return false;
  }
}

/** The platform package for this machine, as @krate/core picks it. */
export function platformFor(platforms, { platform = process.platform, arch = process.arch, musl = isMusl() } = {}) {
  const wantMusl = platform === 'linux' && arch === 'x64' && musl;
  return platforms.find((t) => t.os === platform && t.cpu === arch && !!t.musl === wantMusl);
}
