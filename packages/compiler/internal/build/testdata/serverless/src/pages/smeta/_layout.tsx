export const metadata = {
  title: { template: "%s - Static", default: "Static" },
  metadataBase: "https://static.example.com",
  openGraph: { siteName: "Static site" },
};

export default function SMetaLayout({ children }: any) {
  return <div id="smeta-layout">{children}</div>;
}
