import { ImageResponse } from "@krate/runtime/og";

export const alt = "A post card";

export default async function Image({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  return new ImageResponse(
    <div style={{ width: "100%", height: "100%", display: "flex", flexDirection: "column", justifyContent: "flex-end", padding: 60, backgroundColor: slug === "alpha" ? "#065f46" : "#7c2d12", color: "white" }}>
      <div style={{ fontSize: 40, opacity: 0.8 }}>Krate blog</div>
      <div style={{ fontSize: 110, fontWeight: 700 }}>{slug}</div>
    </div>,
    { width: 1200, height: 630 },
  );
}
