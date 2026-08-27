interface VideoProps {
  params?: { id: string }
}

export default function VideoPage(props: VideoProps) {
  const videoId = props.params?.id || 'unknown'

  return (
    <div class="page">
      <Head>
        <title>Video: {videoId}</title>
        <meta name="description" content={`Watch video ${videoId}`} />
      </Head>

      <h1>Video Player</h1>
      <div class="card">
        <p>Video ID: <strong>{videoId}</strong></p>
        <p>This page uses a dynamic [id] route segment.</p>
      </div>

      <h2>Try other videos</h2>
      <ul>
        <li><Link href="/video/abc123">Video abc123</Link></li>
        <li><Link href="/video/demo-42">Video demo-42</Link></li>
        <li><Link href="/video/hello-world">Video hello-world</Link></li>
      </ul>

      <Link href="/">← Back to Home</Link>
    </div>
  )
}
