// Shapes of the /__krate/dev endpoints (see the dev server's devapi.go).

export interface BuildPhase {
  name: string;
  ms: number;
}

export interface BuildRecord {
  id: number;
  start: number;
  durationMs: number;
  kind: string;
  files: string[];
  routes: string[];
  pages: number;
  ok: boolean;
  diagnostics: number;
  phases?: BuildPhase[];
}

export interface ActivityData {
  builds: BuildRecord[];
  lastGoodAt?: number;
  building?: boolean;
}

export interface DevInfo {
  version?: string;
  startedAt?: number;
  logFile?: string;
  docsURL?: string;
  editor?: { configured?: string; available?: { id: string; label: string }[] };
}

export interface RouteData {
  path?: string;
  pattern: string;
  source?: string;
  mode?: string;
  revalidate?: number;
  params?: Record<string, string>;
  dynamic?: boolean;
  staticPaths?: number;
  layouts?: string[];
  sizes?: {
    html?: number;
    js?: number;
    css?: number;
    gzipHtml?: number;
    gzipJs?: number;
    gzipCss?: number;
    gzipRuntime?: number;
    gzipIslands?: number;
  };
  islands?: { id: string; chunk: string; gzip: number }[];
  budget?: { js?: number; hydration?: number; islandJS?: number; route?: number };
  builtAt?: number;
  document?: string;
}

export interface LogLine {
  seq: number;
  t: number;
  src: string;
  lvl: 'info' | 'warn' | 'error';
  text: string;
  /** Captured in this tab rather than by the dev server. */
  local?: boolean;
}

export interface FixChange {
  file: string;
  line: number;
  col: number;
  code: string;
  from: string;
  to: string;
}

export const DEFAULT_DOCS_URL = 'https://krate.js.org/docs/reference/diagnostics/#';
