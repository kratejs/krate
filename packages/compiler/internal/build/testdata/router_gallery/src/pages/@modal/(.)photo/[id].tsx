export default function PhotoModal({ params }) {
  return <div id="modal" role="dialog">Modal photo {params.id} <Link href="/photo/2">next</Link></div>;
}
