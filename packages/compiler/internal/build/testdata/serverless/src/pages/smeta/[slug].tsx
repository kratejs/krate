export function generateStaticParams() {
  return [{ slug: "one" }, { slug: "two" }];
}

export async function generateMetadata({ params }: any) {
  const { slug } = await params;
  return {
    title: "Entry " + slug,
    description: "about " + slug,
    openGraph: { title: "OG " + slug, images: ["/img/" + slug + ".png"] },
  };
}

export default function Entry({ params }: any) {
  return <h1 id="smeta-page">entry {params.slug}</h1>;
}
