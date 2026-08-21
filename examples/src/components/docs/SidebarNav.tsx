interface SidebarItem {
  title: string;
  url: string;
  active?: boolean;
  indexURL?: string;
  collapsible?: boolean;
  expanded?: boolean;
  children?: SidebarItem[];
}

interface SidebarSectionProps {
  item: SidebarItem;
  currentPath: string;
}

function SidebarSection({ item, currentPath }: SidebarSectionProps) {
  const hasChildren = item.children && item.children.length > 0;
  const isCollapsible = item.collapsible;
  const isExpanded = item.expanded;
  const isActive = item.active;

  if (!hasChildren && item.url) {
    return (
      <a
        class={`sidebar-link${isActive ? ' active' : ''}`}
        href={item.url}
      >
        {item.title}
      </a>
    );
  }

  const sectionClass = `sidebar-section${isCollapsible ? ' sidebar-collapsible' : ' sidebar-section-static'}${isExpanded ? ' open' : ' collapsed'}${isActive ? ' active' : ''}`;

  return (
    <div class={sectionClass}>
      <div class="sidebar-section-row">
        {item.indexURL ? (
          <a
            class={`sidebar-section-link${item.indexURL === currentPath ? ' active' : ''}`}
            href={item.indexURL}
          >
            {item.title}
          </a>
        ) : (
          <span class="sidebar-section-link">{item.title}</span>
        )}
        {isCollapsible && (
          <button
            class="sidebar-section-toggle"
            type="button"
            aria-label={`Toggle ${item.title}`}
            aria-expanded={isExpanded ? 'true' : 'false'}
          >
            <span class="sidebar-chevron">&rsaquo;</span>
          </button>
        )}
      </div>
      <div class="sidebar-section-children">
        {item.children && item.children.map((child) => (
          <SidebarSection
            item={child}
            currentPath={currentPath}
          />
        ))}
      </div>
    </div>
  );
}

interface SidebarNavProps {
  items: SidebarItem[];
  currentPath: string;
}

export default function SidebarNav({ items, currentPath }: SidebarNavProps) {
  return (
    <div class="sidebar-nav">
      {items.map((item) => (
        <SidebarSection item={item} currentPath={currentPath} />
      ))}
    </div>
  );
}
