import { ImageResponse } from "@krate/runtime/og";

export const alt = "The home card";
export const size = { width: 1200, height: 630 };

export default function Image() {
  return new ImageResponse(
    <div style={{ width: "100%", height: "100%", display: "flex", alignItems: "center", justifyContent: "center", backgroundColor: "#0b3d91", color: "white", fontSize: 96, fontWeight: 700 }}>
      Home card
    </div>,
    size,
  );
}
