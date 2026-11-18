import { readFileSync } from 'node:fs';

export const dynamic = 'force-dynamic';

// How many times register() ran in this server.
export async function GET() {
  const marker = process.env.INSTR_MARKER;
  const text = marker ? readFileSync(marker, 'utf8') : '';
  return Response.json({ registers: text.split('\n').filter(Boolean).length });
}
