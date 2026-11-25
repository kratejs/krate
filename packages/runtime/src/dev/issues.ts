// The issues the error overlay pages through, and their copy-for-AI text.

import { errorName, firstAppFrame, parseStack, projectLocation } from './stack.js';

export interface DevDiagnostic {
  file?: string;
  line?: number;
  col?: number;
  message: string;
  hint?: string;
  source?: string;
  severity?: string;
  code?: string;
}

export interface DevClientError {
  message: string;
  stack?: string;
  url?: string;
  line?: number;
  col?: number;
  kind?: string;
  /** The window or view id the page was told it is in (Krate Native). */
  window?: string;
  source?: string;
  detail?: string;
  exitCode?: number;
  log?: string[];
  restart?: string;
  /** The sidecar's runtime and version ("node 22.11"). */
  runtime?: string;
  /** How often it failed and was restarted within the last minute. */
  restarts?: number;
}

export type IssueKind = 'build' | 'runtime' | 'server';

export interface Issue {
  id: string;
  kind: IssueKind;
  severity: 'error' | 'warning';
  /** The chip: a diagnostic code, an error class, or the failing part. */
  label: string;
  message: string;
  file?: string;
  line?: number;
  col?: number;
  hint?: string;
  /** The offending source line a diagnostic carries. */
  source?: string;
  code?: string;
  stack?: string;
  hostStack?: string;
  /** The location is a bundle the page loaded, not a project file. */
  served?: boolean;
  /** How the runtime error was caught. */
  origin?: 'error' | 'rejection' | 'console' | 'resource' | 'remote';
  count: number;
  time: number;
  server?: DevClientError;
}

export interface CodeFrame {
  file: string;
  lang: string;
  start: number;
  lines: string[];
  /** Hydration scope key of the client component around the line (built output only). */
  scope?: string;
}

export function issueFromDiagnostic(d: DevDiagnostic, i: number, time: number): Issue {
  const warning = d.severity === 'warning';
  return {
    // A warning keeps its id across builds (it can be ignored for the
    // session); errors are numbered as reported.
    id: (warning ? 'w:' + (d.col || 0) + ':' : 'b' + i + ':') + (d.code || '') + ':' + (d.file || '') + ':' + (d.line || 0),
    kind: 'build',
    severity: warning ? 'warning' : 'error',
    label: d.code || (warning ? 'Warning' : 'Build error'),
    message: d.message.replace(/^UI: /, ''),
    file: d.file,
    line: d.line,
    col: d.col,
    hint: d.hint,
    source: d.source,
    code: d.code,
    count: 1,
    time,
  };
}

/** True for a build warning (it never blocks the build or opens the overlay). */
export function isBuildWarning(i: Issue): boolean {
  return i.kind === 'build' && i.severity === 'warning';
}

/**
 * Build issues from a list of diagnostics, one per location: a component
 * used by several pages reports the same diagnostic once per page.
 */
export function issuesFromDiagnostics(diags: DevDiagnostic[], time: number, offset = 0): Issue[] {
  const out: Issue[] = [];
  const byKey = new Map<string, Issue>();
  diags.forEach((x, i) => {
    if (!x || typeof x.message !== 'string') return;
    const k = [x.code, x.file, x.line, x.col, x.message, x.severity].join('|');
    const dup = byKey.get(k);
    if (dup) {
      dup.count++;
      return;
    }
    const issue = issueFromDiagnostic(x, offset + i, time);
    byKey.set(k, issue);
    out.push(issue);
  });
  return out;
}

const ORIGIN_LABEL: Record<string, string> = {
  console: 'console.error',
  resource: 'Resource error',
  rejection: 'Unhandled rejection',
};

export function issueFromRuntime(
  origin: NonNullable<Issue['origin']>,
  message: string,
  stack: string | undefined,
  hostStack: string | undefined,
  time: number,
): Issue {
  const frame = firstAppFrame(parseStack(stack));
  const label = ORIGIN_LABEL[origin] || errorName(stack, message) || 'Error';
  return {
    id: 'r:' + message + '|' + (stack || ''),
    kind: 'runtime',
    severity: origin === 'console' ? 'warning' : 'error',
    label,
    message: cleanMessage(message, label),
    file: frame?.file,
    line: frame?.line,
    col: frame?.col,
    served: frame?.served,
    stack,
    hostStack,
    origin,
    count: 1,
    time,
  };
}

/** Drops the browser's "Uncaught " prefix and an error class the chip already shows. */
export function cleanMessage(message: string, label: string): string {
  let m = message.replace(/^Uncaught (?:\(in promise\) )?/, '');
  if (label && m.startsWith(label + ': ')) m = m.slice(label.length + 2);
  return m;
}

export function issueFromServer(ce: DevClientError, time: number): Issue {
  // The stack ends with the console tail; an old line there is not this crash.
  const tail = new Set((ce.log || []).map((l) => l.trim()));
  const own = (ce.stack || '')
    .split('\n')
    .filter((l) => !tail.has(l.trim()))
    .join('\n');
  const loc = projectLocation(own);
  const source = ce.source || 'Server';
  return {
    id: 's:' + source,
    kind: 'server',
    severity: 'error',
    label: errorName(ce.stack, ce.detail) || source,
    message: ce.detail || ce.message,
    file: loc?.file,
    line: loc?.line,
    col: loc?.col,
    stack: ce.stack,
    count: 1,
    time,
    server: ce,
  };
}

/** "1 issue in 1 file", "3 issues in 2 files". */
export function issueSummary(issues: Issue[]): string {
  const files = new Set(issues.map((i) => i.file).filter(Boolean));
  const n = issues.length;
  const word = n && issues.every((i) => i.severity === 'warning') ? 'warning' : 'issue';
  return n + ' ' + word + (n === 1 ? '' : 's') + (files.size ? ' in ' + files.size + (files.size === 1 ? ' file' : ' files') : '');
}

function fence(lang: string, body: string): string {
  const ticks = body.includes('```') ? '````' : '```';
  return ticks + lang + '\n' + body.replace(/\n+$/, '') + '\n' + ticks;
}

/** The code frame as plain text with line numbers and a caret under the column. */
export function frameText(frame: CodeFrame, line?: number, col?: number): string {
  const last = frame.start + frame.lines.length - 1;
  const w = String(last).length;
  const out: string[] = [];
  frame.lines.forEach((text, i) => {
    const n = frame.start + i;
    const hl = n === line;
    out.push((hl ? '> ' : '  ') + String(n).padStart(w) + ' | ' + text);
    if (hl && col && col > 0) out.push('  ' + ' '.repeat(w) + ' | ' + ' '.repeat(col - 1) + '^');
  });
  return out.join('\n');
}

/** Markdown describing an issue, for pasting into an AI assistant or a bug report. */
export function copyForAI(issue: Issue, frame: CodeFrame | null, page?: { url: string; route?: string }): string {
  const kind = issue.kind === 'build' ? 'Build error' : issue.kind === 'server' ? 'Server error' : 'Runtime error';
  const out: string[] = [];
  out.push('## ' + kind + (issue.label ? ' ' + issue.label : '') + ': ' + issue.message.split('\n')[0]);
  out.push('');
  if (issue.message.includes('\n')) out.push(issue.message, '');
  const loc = issue.file ? issue.file + (issue.line ? ':' + issue.line + (issue.col ? ':' + issue.col : '') : '') : '';
  if (loc) out.push('- Location: `' + loc + '`');
  if (issue.server) {
    out.push('- Failed: ' + (issue.server.source || 'server'));
    if (issue.server.exitCode !== undefined) out.push('- Exit code: ' + issue.server.exitCode);
  }
  if (page) out.push('- Page: ' + page.url + (page.route && page.route !== page.url ? ' (route `' + page.route + '`)' : ''));
  if (issue.count > 1) out.push('- Happened ' + issue.count + ' times');
  out.push('- Reported by: Krate dev server');
  if (frame && frame.lines.length) {
    out.push('', fence(frame.lang || '', frameText(frame, issue.line, issue.col)));
  } else if (issue.source) {
    out.push('', fence('', issue.source + (issue.col ? '\n' + ' '.repeat(issue.col - 1) + '^' : '')));
  }
  if (issue.hint) out.push('', 'Hint: ' + issue.hint);
  if (issue.stack && issue.kind !== 'build') out.push('', 'Stack:', '', fence('', issue.stack));
  if (issue.hostStack) out.push('', 'Host stack:', '', fence('', issue.hostStack));
  if (issue.server?.log?.length) out.push('', 'Last console lines:', '', fence('', issue.server.log.join('\n')));
  return out.join('\n') + '\n';
}
