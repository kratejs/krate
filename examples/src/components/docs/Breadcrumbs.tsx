interface BreadcrumbItem {
  label: string;
  url: string;
  isLast: boolean;
}

interface BreadcrumbsProps {
  items: BreadcrumbItem[];
}

export default function Breadcrumbs({ items }: BreadcrumbsProps) {
  if (!items || items.length === 0) return <span />;

  return (
    <nav class="breadcrumbs">
      {items.map((item, i) => (
        <span>
          {i > 0 && <span class="sep"> / </span>}
          {item.isLast ? (
            <span class="current">{item.label}</span>
          ) : (
            <a href={`/docs${item.url}`}>{item.label}</a>
          )}
        </span>
      ))}
    </nav>
  );
}
