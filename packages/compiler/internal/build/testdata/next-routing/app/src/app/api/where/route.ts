import { where } from '../../../lib/meta/where';

export const dynamic = 'force-dynamic';

export async function GET() {
  return Response.json(where());
}
