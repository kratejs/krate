export default async function Template({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  return <main><h1>Template {slug}</h1></main>;
}
