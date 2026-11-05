import { useSelectedLayoutSegment, useSelectedLayoutSegments } from "@krate/runtime";

export default function DashLayout({ children }: any) {
  const seg = useSelectedLayoutSegment();
  const segs = useSelectedLayoutSegments();
  return (
    <section id="dash-layout">
      <i id="dash-seg">{String(seg)}</i>
      <b id="dash-segs">{segs.join("|")}</b>
      {children}
    </section>
  );
}
