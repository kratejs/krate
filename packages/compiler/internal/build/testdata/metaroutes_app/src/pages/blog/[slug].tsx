export function generateStaticParams() { return [{ slug: "alpha" }, { slug: "beta" }]; }
export const metadata = { title: "Post" };
export default function Post({ params }: any) { return <h1>{params.slug}</h1>; }
