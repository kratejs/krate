import type { MetadataRoute } from "@krate/runtime/server";

export default function sitemap(): MetadataRoute.Sitemap {
  return [
    { url: "https://convention.example/", lastModified: new Date("2026-01-02T00:00:00Z"), changeFrequency: "weekly", priority: 1 },
    { url: "https://convention.example/blog/a/", alternates: { languages: { de: "https://convention.example/de/blog/a/" } } },
  ];
}
