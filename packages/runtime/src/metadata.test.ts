import { describe, expect, it } from 'vitest';
import { renderMetadataTags, resolveMetadata } from './metadata.js';

async function head(chain: Array<Record<string, unknown>>): Promise<string> {
  const r = await resolveMetadata(
    chain.map((metadata) => ({ mod: { metadata } })) as never,
    { params: {}, searchParams: {} } as never,
  );
  return renderMetadataTags(r.metadata, r.templates);
}

describe('openGraph and twitter inheritance', () => {
  it('fills og:title from the resolved title, not og:description from the description', async () => {
    const out = await head([
      { title: 'Site', openGraph: { siteName: 'Site' } },
      { title: 'Home page', description: 'About home' },
    ]);
    expect(out).toContain('<title>Home page</title>');
    expect(out).toContain('og:title" content="Home page"');
    expect(out).not.toContain('og:description');
    expect(out).toContain('og:site_name');
  });

  it('uses the templated top-level title, not the template again', async () => {
    const out = await head([
      { title: { default: 'Site', template: '%s | Site' }, openGraph: { siteName: 'Site' } },
      { title: 'Docs' },
    ]);
    expect(out).toContain('<title>Docs | Site</title>');
    expect(out).toContain('og:title" content="Docs | Site"');
  });

  it('keeps an explicit og title and lets twitter follow it', async () => {
    const out = await head([{ title: 'Top', description: 'D', openGraph: { title: 'OG', description: 'OGD' }, twitter: { card: 'summary' } }]);
    expect(out).toContain('og:title" content="OG"');
    expect(out).toContain('twitter:title" content="OG"');
    expect(out).toContain('twitter:description" content="OGD"');
  });

  it('fills the twitter title from the top level when there is no openGraph', async () => {
    const out = await head([{ title: 'Top', description: 'D', twitter: { card: 'summary' } }]);
    expect(out).toContain('twitter:title" content="Top"');
    expect(out).not.toContain('twitter:description');
    expect(out).not.toContain('og:title');
  });

  it('emits nothing extra without openGraph or twitter', async () => {
    const out = await head([{ title: 'Top' }]);
    expect(out).not.toContain('og:');
    expect(out).not.toContain('twitter:');
  });
});
