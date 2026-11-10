export async function POST(request) { return new Response((await request.text()).toUpperCase(), { headers: { "x-custom": "yes" } }); }
