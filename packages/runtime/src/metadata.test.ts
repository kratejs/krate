// @vitest-environment node
import { describe, expect, it, vi } from 'vitest';
import { metadataHeadTags, renderMetadataTags, resolveMetadata, type MetadataSegment } from './metadata';
import { forbidden, notFound } from './request-context';

const tags = async (segments: MetadataSegment[], props: any = {}) => metadataHeadTags(segments, props);

describe('resolveMetadata merge order', () => {
  it('merges root layout to page: a deeper segment replaces a field wholesale', async () => {
    const { metadata } = await resolveMetadata(
      [
        { mod: { metadata: { description: 'root', openGraph: { siteName: 'Site', title: 'root og' }, robots: 'noindex' } } },
        { mod: { metadata: { description: 'section' } } },
        { mod: { metadata: { openGraph: { title: 'page og' } } } },
      ],
      {},
    );
    expect(metadata.description).toBe('section');
    // openGraph is replaced, not merged: siteName does not leak into the page.
    expect(metadata.openGraph).toEqual({ title: 'page og' });
    expect(metadata.robots).toBe('noindex');
  });

  it('applies the nearest title template to a title string, not to absolute or default', async () => {
    const r = await resolveMetadata(
      [
        { mod: { metadata: { title: { default: 'Site', template: '%s | Site' } } } },
        { mod: { metadata: { title: 'About' } } },
      ],
      {},
    );
    expect(r.metadata.title).toBe('About | Site');
    const abs = await resolveMetadata([{ mod: { metadata: { title: { template: '%s | Site', default: 'Site' } } } }, { mod: { metadata: { title: { absolute: 'Exactly' } } } }], {});
    expect(abs.metadata.title).toBe('Exactly');
    const own = await resolveMetadata([{ mod: { metadata: { title: { template: '%s | Site', default: 'Site' } } } }], {});
    expect(own.metadata.title).toBe('Site');
    const nested = await resolveMetadata(
      [
        { mod: { metadata: { title: { template: '%s | A', default: 'A' } } } },
        { mod: { metadata: { title: { template: '%s | B', default: 'B' } } } },
        { mod: { metadata: { title: 'Page' } } },
      ],
      {},
    );
    expect(nested.metadata.title).toBe('Page | B');
  });

  it('calls generateMetadata with awaitable params and searchParams and the parent metadata', async () => {
    const seen: any[] = [];
    const { metadata } = await resolveMetadata(
      [
        { mod: { metadata: { description: 'from layout', title: { default: 'D', template: '%s!' } } } },
        {
          mod: {
            async generateMetadata(props: any, parent: Promise<any>) {
              const params = await props.params;
              const sp = await props.searchParams;
              const above = await parent;
              seen.push([props.params.slug, params.slug, sp.q, above.description]);
              return { title: 'Post ' + params.slug };
            },
          },
        },
      ],
      { params: { slug: 'x' }, searchParams: { q: '1' } },
    );
    expect(seen).toEqual([['x', 'x', '1', 'from layout']]);
    expect(metadata.title).toBe('Post x!');
  });

  it('skips a generateMetadata that throws and keeps the rest', async () => {
    const r = await resolveMetadata(
      [
        { mod: { metadata: { description: 'layout' } }, file: 'layout' },
        {
          mod: {
            generateMetadata() {
              throw new Error('lookup failed');
            },
            metadata: { title: 'ignored when the function exists' },
          },
          file: 'page',
        },
      ],
      {},
    );
    expect(r.metadata).toEqual({ description: 'layout' });
    expect(r.errors).toHaveLength(1);
    expect(r.errors[0].file).toBe('page');
  });

  it('lets notFound(), forbidden() and redirects through', async () => {
    await expect(resolveMetadata([{ mod: { generateMetadata: () => notFound() } }], {})).rejects.toMatchObject({ digest: 'NEXT_NOT_FOUND' });
    await expect(resolveMetadata([{ mod: { generateMetadata: () => forbidden() } }], {})).rejects.toMatchObject({ __krateHTTPStatus: 403 });
    await expect(
      resolveMetadata([{ mod: { generateMetadata: () => { throw Object.assign(new Error('r'), { __krateRedirect: '/x' }); } } }], {}),
    ).rejects.toMatchObject({ __krateRedirect: '/x' });
  });

  it('logs a failing generateMetadata and still returns the tags', async () => {
    const log = vi.fn();
    const out = await metadataHeadTags(
      [{ mod: { metadata: { description: 'kept' } } }, { mod: { generateMetadata: () => { throw new Error('nope'); } }, file: 'page.tsx' }],
      {},
      log,
    );
    expect(out).toContain('<meta name="description" content="kept">');
    expect(log).toHaveBeenCalledOnce();
    expect(log.mock.calls[0][0]).toContain('page.tsx');
  });

  it('fills the fields a file convention supplies unless the segment sets them', async () => {
    const files = { openGraph: { images: ['/og.png'] }, twitter: { images: ['/tw.png'] } };
    const a = await resolveMetadata([{ mod: { metadata: { title: 'T' } }, files }], {});
    expect(a.metadata.openGraph).toEqual({ images: ['/og.png'] });
    expect(a.metadata.twitter).toMatchObject({ card: 'summary_large_image', images: ['/tw.png'] });
    const b = await resolveMetadata([{ mod: { metadata: { openGraph: { images: ['/mine.png'] } } }, files }], {});
    expect(b.metadata.openGraph).toEqual({ images: ['/mine.png'] });
  });
});

describe('renderMetadataTags', () => {
  it('renders the common fields', () => {
    const html = renderMetadataTags({
      title: 'Hello',
      description: 'A <b>"post"</b>',
      keywords: ['a', 'b'],
      authors: [{ name: 'Ada', url: 'https://ada.example' }],
      robots: { index: false, follow: true, googleBot: { index: true, maxImagePreview: 'large' } },
      alternates: { canonical: '/hello', languages: { 'en-US': '/en', de: '/de' } },
      themeColor: [{ media: '(prefers-color-scheme: dark)', color: '#000' }],
      metadataBase: 'https://example.com',
      icons: { icon: '/favicon.ico', apple: [{ url: '/apple.png', sizes: '180x180' }] },
      manifest: '/site.webmanifest',
      other: { custom: 'x' },
    });
    expect(html).toContain('<title>Hello</title>');
    expect(html).toContain('<meta name="description" content="A &lt;b&gt;&quot;post&quot;&lt;/b&gt;">');
    expect(html).toContain('<meta name="keywords" content="a,b">');
    expect(html).toContain('<meta name="author" content="Ada">');
    expect(html).toContain('<link rel="author" href="https://ada.example">');
    expect(html).toContain('<meta name="robots" content="noindex, follow">');
    expect(html).toContain('<meta name="googlebot" content="index, follow, max-image-preview:large">');
    expect(html).toContain('<link rel="canonical" href="https://example.com/hello">');
    expect(html).toContain('<link rel="alternate" hreflang="en-US" href="https://example.com/en">');
    expect(html).toContain('<meta name="theme-color" content="#000" media="(prefers-color-scheme: dark)">');
    expect(html).toContain('<link rel="icon" href="https://example.com/favicon.ico">');
    expect(html).toContain('<link rel="apple-touch-icon" href="https://example.com/apple.png" sizes="180x180">');
    expect(html).toContain('<link rel="manifest" href="https://example.com/site.webmanifest">');
    expect(html).toContain('<meta name="custom" content="x">');
  });

  it('renders Open Graph and Twitter with images resolved against metadataBase', () => {
    const html = renderMetadataTags({
      metadataBase: 'https://example.com/docs',
      openGraph: {
        type: 'article',
        title: 'T',
        url: '/a',
        images: ['/i.png', { url: 'https://cdn.example/x.png', width: 800, height: 600, alt: 'x' }],
        publishedTime: '2026-01-01',
        authors: ['Ada'],
        tags: ['a', 'b'],
      },
      twitter: { title: 'TT', images: ['/t.png'], creator: '@ada' },
    });
    expect(html).toContain('<meta property="og:url" content="https://example.com/docs/a">');
    expect(html).toContain('<meta property="og:image" content="https://example.com/docs/i.png">');
    expect(html).toContain('<meta property="og:image" content="https://cdn.example/x.png">');
    expect(html).toContain('<meta property="og:image:width" content="800">');
    expect(html).toContain('<meta property="og:image:alt" content="x">');
    expect(html).toContain('<meta property="article:published_time" content="2026-01-01">');
    expect(html).toContain('<meta property="article:tag" content="b">');
    expect(html).toContain('<meta name="twitter:card" content="summary_large_image">');
    expect(html).toContain('<meta name="twitter:image" content="https://example.com/docs/t.png">');
    expect(html).toContain('<meta name="twitter:creator" content="@ada">');
  });

  it('gives openGraph and twitter titles their own templates', async () => {
    const out = await tags([
      { mod: { metadata: { title: { default: 'D', template: '%s | Page' }, openGraph: { title: { default: 'OG', template: '%s | OG' } } } } },
      { mod: { metadata: { title: 'P', openGraph: { title: 'P' } } } },
    ]);
    expect(out).toContain('<title>P | Page</title>');
    expect(out).toContain('<meta property="og:title" content="P | OG">');
  });

  it('renders a viewport export with themeColor and colorScheme', async () => {
    const out = await tags([
      { mod: { viewport: { width: 'device-width', initialScale: 1, maximumScale: 5, userScalable: false, themeColor: '#fff', colorScheme: 'light dark' } } },
    ]);
    expect(out).toContain('<meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=5, user-scalable=no">');
    expect(out).toContain('<meta name="theme-color" content="#fff">');
    expect(out).toContain('<meta name="color-scheme" content="light dark">');
  });

  it('renders nothing for an empty chain', async () => {
    expect(await tags([])).toBe('');
    expect(await tags([{ mod: {} }])).toBe('');
  });
});
