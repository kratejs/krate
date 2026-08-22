export function GET(request: Request) {
  return Response.json({ message: 'hello', id: 42 });
}
