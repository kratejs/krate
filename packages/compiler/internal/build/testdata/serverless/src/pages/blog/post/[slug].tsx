export const dynamicParams = false;
export function generateStaticParams() {
  return [{ slug: "a" }, { slug: "b" }];
}
export default function ClosedPost({ params }: any) {
  return <p id="page">closed {params.slug}</p>;
}
