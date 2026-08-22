interface TOCItem {
  title: string;
  id: string;
  depth: number;
}

interface TOCNavProps {
  items: TOCItem[];
}

export default function TOCNav({ items }: TOCNavProps) {
  if (!items || items.length === 0) return <span />;

  return (
    <nav class="toc-nav">
      {items.map((item) => (
        <a
          class={`toc-link${item.depth <= 2 ? ' toc-h2' : ' toc-h3'}`}
          data-depth={item.depth}
          href={`#${item.id}`}
        >
          {item.title}
        </a>
      ))}
    </nav>
  );
}
