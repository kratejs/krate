export const metadata = {
  title: { template: "%s | Krate", default: "Meta" },
  description: "layout description",
  openGraph: { siteName: "Krate" },
};

export default function MetaLayout({ children }: any) {
  return <section id="meta-layout">{children}</section>;
}
