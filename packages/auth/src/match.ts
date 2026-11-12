const cache = new Map<string, RegExp>();

function escapeLiteral(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

export function compilePattern(source: string): RegExp {
  const cached = cache.get(source);
  if (cached) return cached;
  const trimmed = source.length > 1 ? source.replace(/\/+$/, "") : source;
  const segments = trimmed.split("/").filter((s) => s.length > 0);
  let body = "";
  let open = false;
  for (let i = 0; i < segments.length; i++) {
    const segment = segments[i]!;
    const last = i === segments.length - 1;
    if (segment === "**" || /^:[A-Za-z_][\w]*\*$/.test(segment)) {
      body += "(?:/.*)?";
      open = true;
      if (!last) break;
    } else if (segment === "*") {
      body += "/[^/]+";
      if (last) open = true;
    } else if (/^:[A-Za-z_][\w]*$/.test(segment)) {
      body += "/[^/]+";
    } else if (/^:[A-Za-z_][\w]*\+$/.test(segment)) {
      body += "/.+";
      open = true;
    } else if (/^:[A-Za-z_][\w]*\?$/.test(segment)) {
      body += "(?:/[^/]+)?";
    } else {
      body += "/" + escapeLiteral(segment);
    }
  }
  const regex = new RegExp(`^${body === "" ? "" : body}${open ? "" : "(?:/.*)?"}/?$`);
  cache.set(source, regex);
  return regex;
}

export function matchesPath(source: string, pathname: string): boolean {
  const path = pathname.length > 1 ? pathname.replace(/\/+$/, "") : pathname;
  if (source === "/" || source === "") return true;
  return compilePattern(source).test(path === "" ? "/" : path);
}
