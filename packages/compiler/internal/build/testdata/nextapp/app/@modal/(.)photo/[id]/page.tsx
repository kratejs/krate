import Link from "next/link";

export default function PhotoModal({ params }: { params: { id: string } }) {
  return (
    <dialog id="modal" open>
      <p>Modal photo {params.id}</p>
      <Link href="/">Close</Link>
    </dialog>
  );
}
