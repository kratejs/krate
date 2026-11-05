export default function HeadLayout({ children }: any) {
  return (
    <section id="head-layout">
      <Head>
        <meta name="description" content="layout description" />
        <meta property="og:site_name" content="Krate Site" />
        <link rel="canonical" href="https://example.com/layout" />
      </Head>
      {children}
    </section>
  );
}
