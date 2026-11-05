import type { MetadataRoute } from "@krate/runtime/server";

export default function robots(): MetadataRoute.Robots {
  return {
    rules: [{ userAgent: "*", allow: "/", disallow: ["/private/"] }, { userAgent: "BadBot", disallow: "/" }],
    sitemap: "https://convention.example/sitemap.xml",
  };
}
