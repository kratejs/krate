// Reads a local image for ImageResponse. This is the only Node-file-system
// use in the image path; the Cloudflare and other non-Node server bundles
// replace this module with a stub (see serverdeploy.go), because per-request
// image reads happen on the Krate host, never in a Worker.

/** Returns the bytes of src (under public/ or the project root), or null when absent. */
export async function readLocalImage(src: string, root: string): Promise<Uint8Array | null> {
  const fs = await import('node:fs/promises');
  const path = await import('node:path');
  const rel = src.replace(/^\//, '');
  for (const base of [path.join(root, 'public', rel), path.join(root, rel), src]) {
    try {
      return await fs.readFile(base);
    } catch {
      /* try the next place */
    }
  }
  return null;
}
