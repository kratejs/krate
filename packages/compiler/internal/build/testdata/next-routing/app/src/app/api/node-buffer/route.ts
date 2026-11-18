import { encode } from '../../../lib/encode';

export const dynamic = 'force-dynamic';

export async function GET() {
  return Response.json({ route: Buffer.from('hello', 'utf8').toString('base64'), helper: encode('world') });
}
