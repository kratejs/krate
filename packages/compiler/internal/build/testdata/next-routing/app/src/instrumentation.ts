import { appendFileSync } from 'node:fs';

// Appends one line per register() call to $INSTR_MARKER, so a test can count the runs.
export async function register() {
  const marker = process.env.INSTR_MARKER;
  if (marker) appendFileSync(marker, `register\n`);
}
