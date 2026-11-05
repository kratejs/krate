import { ImageResponse } from "@krate/runtime/og";

export const alt = "The twitter card";

export default function Image() {
  return new ImageResponse(
    <div style={{ width: "100%", height: "100%", display: "flex", backgroundImage: "linear-gradient(90deg, #ef4444, #f59e0b)", padding: 80, color: "white", fontSize: 72 }}>Twitter</div>,
    { width: 1200, height: 600 },
  );
}
