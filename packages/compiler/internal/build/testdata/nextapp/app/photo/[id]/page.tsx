export default function PhotoPage({ params }: { params: { id: string } }) {
  return <main id="photo-page"><h1>Photo {params.id}</h1></main>;
}
