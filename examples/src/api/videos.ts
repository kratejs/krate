type Video = {
  id: string
  title: string
  duration: number
}

const videos: Video[] = [
  { id: 'abc123', title: 'Intro to Krate', duration: 300 },
  { id: 'demo-42', title: 'Advanced Patterns', duration: 600 },
  { id: 'hello-world', title: 'Hello World Tutorial', duration: 180 },
]

export function GET(request: Request) {
  const url = new URL(request.url)
  const id = url.searchParams.get('id')

  if (id) {
    const video = videos.find(v => v.id === id)
    if (!video) {
      return Response.json({ error: 'Video not found' }, { status: 404 })
    }
    return Response.json(video)
  }

  return Response.json({ videos, count: videos.length })
}

export async function POST(request: Request) {
  const body = await request.json() as { title: string; duration: number }

  if (!body.title || !body.duration) {
    return Response.json({ error: 'title and duration are required' }, { status: 400 })
  }

  const newVideo: Video = {
    id: `vid-${Date.now()}`,
    title: body.title,
    duration: body.duration,
  }
  videos.push(newVideo)

  return Response.json(newVideo, { status: 201 })
}
