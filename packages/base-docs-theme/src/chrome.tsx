import { onMount, onCleanup } from "@krate/runtime";

interface SidebarItem {
  title: string;
  url: string;
  active?: boolean;
  indexURL?: string;
  collapsible?: boolean;
  expanded?: boolean;
  icon?: string;
  badge?: { text: string; variant?: string };
  children?: SidebarItem[];
}

interface SidebarSectionProps {
  item: SidebarItem;
  currentPath: string;
  /** Ancestor-derived key used to persist this section's open/closed state. */
  parentKey?: string;
}

function sectionHasActive(item: SidebarItem, currentPath: string): boolean {
  if (item.active) return true;
  if (item.url && item.url === currentPath) return true;
  if (item.indexURL && item.indexURL === currentPath) return true;
  if (item.children) {
    for (var i = 0; i < item.children.length; i++) {
      if (sectionHasActive(item.children[i], currentPath)) return true;
    }
  }
  return false;
}

function SidebarSection(props: SidebarSectionProps) {
  var item = props.item;
  var currentPath = props.currentPath;
  const hasChildren = item.children && item.children.length > 0;
  const isCollapsible = item.collapsible;
  const containsActive = sectionHasActive(item, currentPath);
  const open = !isCollapsible || item.expanded || containsActive;
  const navKey = (props.parentKey ? props.parentKey + "/" : "") + (item.indexURL || item.title);

  if (!hasChildren && item.url) {
    return (
      <Link
        className={`sidebar-link${containsActive ? ' active' : ''}`}
        href={item.url}
      >
        {item.icon && <span class="sidebar-icon"><Icon name={item.icon} width="16" height="16" /></span>}
        <span class="sidebar-label">{item.title}</span>
        {item.badge && <span class={`sidebar-badge${item.badge.variant ? " sidebar-badge-" + item.badge.variant : ""}`}>{item.badge.text}</span>}
      </Link>
    );
  }

  const sectionClass = `sidebar-section${isCollapsible ? ' sidebar-collapsible' : ' sidebar-section-static'}${open ? ' open' : ' collapsed'}${containsActive ? ' active' : ''}`;

  return (
    <div class={sectionClass} data-nav-key={navKey}>
      <div class="sidebar-section-row">
        {item.indexURL ? (
          <Link
            className={`sidebar-section-link${item.indexURL === currentPath ? ' active' : ''}`}
            href={item.indexURL}
          >
            {item.icon && <span class="sidebar-icon"><Icon name={item.icon} width="16" height="16" /></span>}
            <span class="sidebar-label">{item.title}</span>
            {item.badge && <span class={`sidebar-badge${item.badge.variant ? " sidebar-badge-" + item.badge.variant : ""}`}>{item.badge.text}</span>}
          </Link>
        ) : (
          <span class="sidebar-section-link">
            {item.icon && <span class="sidebar-icon"><Icon name={item.icon} width="16" height="16" /></span>}
            <span class="sidebar-label">{item.title}</span>
            {item.badge && <span class={`sidebar-badge${item.badge.variant ? " sidebar-badge-" + item.badge.variant : ""}`}>{item.badge.text}</span>}
          </span>
        )}
        {isCollapsible && (
          <button
            class="sidebar-section-toggle"
            type="button"
            aria-label={`Toggle ${item.title}`}
            aria-expanded={open ? 'true' : 'false'}
          >
            <span class="sidebar-chevron">&rsaquo;</span>
          </button>
        )}
      </div>
      <div class="sidebar-section-children">
        {item.children && item.children.map((child) => (
          <SidebarSection item={child} currentPath={currentPath} parentKey={navKey} />
        ))}
      </div>
    </div>
  );
}

interface SidebarNavProps {
  items: SidebarItem[];
  currentPath: string;
  onNavigate?: () => void;
}

function normalizePath(value: string): string {
  var path = value || "/";
  try {
    path = new URL(path, window.location.origin).pathname;
  } catch (e) {
    path = String(path).split("#")[0].split("?")[0];
  }
  path = path.replace(/\/index\.html$/, "/");
  if (path.length > 1) path = path.replace(/\/+$/, "");
  return path || "/";
}

export function SidebarNav(props: SidebarNavProps) {
  var items = props.items;
  var currentPath = props.currentPath;
  var onNavigate = props.onNavigate;
  var rootRef: HTMLElement | null = null;

  onMount(function () {
    if (!rootRef) return;
    var el = rootRef;

    function setSectionOpen(section: HTMLElement, open: boolean) {
      var button = section.querySelector(":scope > .sidebar-section-row .sidebar-section-toggle") as HTMLElement | null;
      section.classList.toggle("open", open);
      section.classList.toggle("collapsed", !open);
      if (button) button.setAttribute("aria-expanded", open ? "true" : "false");
    }

    function refreshActive() {
      if (typeof window === "undefined") return;
      var current = normalizePath(window.location.pathname);
      var links = el.querySelectorAll("a[href]");
      for (var i = 0; i < links.length; i++) {
        var linkPath = normalizePath(links[i].getAttribute("href") || "");
        var isActive = linkPath === current;
        links[i].classList.toggle("active", isActive);
        if (isActive) {
          var parent = links[i].closest(".sidebar-section") as HTMLElement | null;
          while (parent) {
            parent.classList.add("active");
            if (parent.classList.contains("sidebar-collapsible")) setSectionOpen(parent, true);
            parent = parent.parentElement ? (parent.parentElement.closest(".sidebar-section") as HTMLElement | null) : null;
          }
        }
      }
    }

    // Folder (section) open/closed state persists across reloads + SPA
    // navigation, keyed by the section's ancestor path.
    var STORAGE_PREFIX = "krate-docs-nav:";
    function readNavState(key: string): string | null {
      try {
        return window.localStorage.getItem(STORAGE_PREFIX + key);
      } catch (e) {
        return null;
      }
    }
    function writeNavState(key: string, open: boolean) {
      try {
        window.localStorage.setItem(STORAGE_PREFIX + key, open ? "open" : "closed");
      } catch (e) {
        /* storage unavailable (private mode) — ignore */
      }
    }
    function restoreNavState() {
      var sections = el.querySelectorAll(".sidebar-collapsible[data-nav-key]");
      for (var i = 0; i < sections.length; i++) {
        var section = sections[i] as HTMLElement;
        // Always reveal the section that contains the current page.
        if (section.classList.contains("active")) {
          setSectionOpen(section, true);
          continue;
        }
        var key = section.getAttribute("data-nav-key");
        if (!key) continue;
        var stored = readNavState(key);
        if (stored === "open") setSectionOpen(section, true);
        else if (stored === "closed") setSectionOpen(section, false);
      }
    }

    function onClick(e: MouseEvent) {
      var target = e.target as HTMLElement;
      var toggle = target.closest(".sidebar-section-toggle") as HTMLElement | null;
      if (toggle) {
        var section = toggle.closest(".sidebar-collapsible") as HTMLElement | null;
        if (section) {
          var willOpen = !section.classList.contains("open");
          setSectionOpen(section, willOpen);
          var key = section.getAttribute("data-nav-key");
          if (key) writeNavState(key, willOpen);
        }
        return;
      }
      var link = target.closest("a[href]") as HTMLElement | null;
      if (link && onNavigate) onNavigate();
    }

    el.addEventListener("click", onClick);
    refreshActive();
    restoreNavState();
  });

  return (
    <div class="sidebar-nav" ref={rootRef}>
      {items.map((item) => (
        <SidebarSection item={item} currentPath={currentPath} />
      ))}
    </div>
  );
}

interface TOCItem {
  title: string;
  id: string;
  depth: number;
}

interface TOCNavProps {
  items: TOCItem[];
  onNavigate?: () => void;
}

interface RailPoint {
  x: number;
  yStart: number;
  yEnd: number;
}

export function TOCNav(props: TOCNavProps) {
  var items = props.items;
  var onNavigate = props.onNavigate;

  var navRef: HTMLElement | null = null;

  onMount(function () {
    if (!navRef || !items || items.length === 0) return;
    var nav = navRef;

    var links = Array.prototype.slice.call(nav.querySelectorAll(".toc-link[href^='#']")) as HTMLElement[];
    if (links.length === 0) return;

    var headings: HTMLElement[] = [];
    var byId: Record<string, HTMLElement> = {};
    for (var i = 0; i < links.length; i++) {
      var href = links[i].getAttribute("href") || "";
      var id = decodeURIComponent(href.slice(1));
      links[i].setAttribute("data-id", id);
      byId[id] = links[i];
      var heading = document.getElementById(id) as HTMLElement | null;
      if (heading) headings.push(heading);
    }

    var ticking = false;
    var activeId = "";
    var currentDir = "down";
    var lastScrollTop = 0;

    // ─── SVG rail indicator ─────────────────────────────────────────────
    var indicatorSvg: SVGSVGElement | null = null;
    var railPath: SVGPathElement | null = null;
    var linePath: SVGPathElement | null = null;
    var circleEl: SVGCircleElement | null = null;
    var visibleHeadingIds: Record<string, boolean> = {};
    var currentPathLength = 0;
    var isAnimating = false;
    var animStart = 0;
    var animEnd = 0;
    var targetStart = 0;
    var targetEnd = 0;

    function tocDepth(link: HTMLElement): number {
      var raw = parseInt(link.getAttribute("data-depth") || "2", 10);
      if (!isFinite(raw)) raw = link.classList.contains("toc-h3") ? 3 : 2;
      return Math.max(2, Math.min(raw, 4));
    }

    function ensureIndicatorSvg() {
      if (indicatorSvg) return;
      // SPA navigation re-runs this component against the kept <nav> element,
      // so a previous mount's manually-appended indicator would otherwise
      // accumulate. Remove any stale indicator before creating a fresh one.
      var stale = nav.querySelectorAll(".toc-indicator-svg");
      for (var s = 0; s < stale.length; s++) {
        stale[s].remove();
      }
      var ns = "http://www.w3.org/2000/svg";
      var svg = document.createElementNS(ns, "svg");
      svg.setAttribute("class", "toc-indicator-svg");
      svg.setAttribute("aria-hidden", "true");
      svg.setAttribute("focusable", "false");
      var rail = document.createElementNS(ns, "path");
      var line = document.createElementNS(ns, "path");
      var circle = document.createElementNS(ns, "circle");
      rail.setAttribute("class", "toc-indicator-rail");
      line.setAttribute("class", "toc-indicator-line");
      circle.setAttribute("class", "toc-indicator-circle");
      circle.setAttribute("r", "2.6");
      svg.appendChild(rail);
      svg.appendChild(line);
      svg.appendChild(circle);
      nav.appendChild(svg);
      indicatorSvg = svg;
      railPath = rail;
      linePath = line;
      circleEl = circle;
    }

    function generateCurvedPath(points: RailPoint[]): string {
      if (!points.length) return "";
      var d = "M " + points[0].x + " " + points[0].yStart + " L " + points[0].x + " " + points[0].yEnd;
      for (var i = 1; i < points.length; i++) {
        var prev = points[i - 1];
        var curr = points[i];
        if (Math.abs(prev.x - curr.x) > 0.5) {
          var midY = prev.yEnd + (curr.yStart - prev.yEnd) * 0.5;
          d += " C " + prev.x + " " + midY + ", " + curr.x + " " + midY + ", " + curr.x + " " + curr.yStart;
        } else {
          d += " L " + curr.x + " " + curr.yStart;
        }
        d += " L " + curr.x + " " + curr.yEnd;
      }
      return d;
    }

    function pointForLink(link: HTMLElement, navRect: DOMRect): RailPoint {
      var rect = link.getBoundingClientRect();
      var depthOffset = (tocDepth(link) - 2) * 12;
      return {
        x: 5 + depthOffset,
        yStart: Math.max(0, rect.top - navRect.top + 6),
        yEnd: Math.max(0, rect.bottom - navRect.top - 6),
      };
    }

    function renderLineLoop() {
      if (!linePath || !circleEl || !currentPathLength) {
        isAnimating = false;
        return;
      }
      var ease = 0.22;
      animStart += (targetStart - animStart) * ease;
      animEnd += (targetEnd - animEnd) * ease;

      var s = Math.max(0, Math.min(currentPathLength, animStart));
      var e = Math.max(s, Math.min(currentPathLength, animEnd));
      linePath.style.strokeDasharray = (e - s) + " " + currentPathLength;
      linePath.style.strokeDashoffset = "-" + s;

      try {
        var circleLen = currentDir === "down" ? e : s;
        var pt = linePath.getPointAtLength(circleLen);
        circleEl.setAttribute("cx", String(pt.x));
        circleEl.setAttribute("cy", String(pt.y));
        circleEl.classList.add("visible");
      } catch (err) {}

      var dist = Math.abs(targetStart - animStart) + Math.abs(targetEnd - animEnd);
      if (dist < 0.1) {
        animStart = targetStart;
        animEnd = targetEnd;
        isAnimating = false;
      } else {
        window.requestAnimationFrame(renderLineLoop);
      }
    }

    function updateLineCoords() {
      if (!links.length || !indicatorSvg || !railPath || !linePath) return;
      var navRect = nav.getBoundingClientRect();
      var railPoints: RailPoint[] = [];
      var targetLinks: HTMLElement[] = [];
      var activeLink = activeId ? byId[activeId] : null;

      for (var i = 0; i < links.length; i++) {
        railPoints.push(pointForLink(links[i], navRect));
        var linkId = links[i].getAttribute("data-id") || "";
        if (visibleHeadingIds[linkId]) targetLinks.push(links[i]);
      }

      if (activeLink && targetLinks.indexOf(activeLink) === -1) targetLinks.push(activeLink);
      targetLinks.sort(function (a, b) {
        return a.getBoundingClientRect().top - b.getBoundingClientRect().top;
      });

      var height = Math.max(nav.scrollHeight, navRect.height, 1);
      indicatorSvg.setAttribute("viewBox", "0 0 34 " + height.toFixed(1));
      indicatorSvg.style.height = height + "px";

      var railD = generateCurvedPath(railPoints);
      railPath.setAttribute("d", railD);
      linePath.setAttribute("d", railD);

      try {
        currentPathLength = linePath.getTotalLength();
      } catch (err) {
        currentPathLength = 0;
      }
      if (!currentPathLength || !targetLinks.length) {
        linePath.style.strokeDasharray = "0 1";
        circleEl.classList.remove("visible");
        return;
      }

      var targetPoints: RailPoint[] = [];
      for (var j = 0; j < targetLinks.length; j++) {
        targetPoints.push(pointForLink(targetLinks[j], navRect));
      }
      var startY = targetPoints[0].yStart;
      var endY = targetPoints[targetPoints.length - 1].yEnd;

      targetStart = 0;
      targetEnd = currentPathLength;
      var foundStart = false;
      var steps = Math.max(80, Math.min(240, Math.round(currentPathLength / 2)));
      for (var step = 0; step <= steps; step++) {
        var len = (step / steps) * currentPathLength;
        var pt = linePath.getPointAtLength(len);
        if (!foundStart && pt.y >= startY - 1) {
          targetStart = len;
          foundStart = true;
        }
        if (pt.y <= endY + 1) targetEnd = len;
      }
      targetEnd = Math.max(targetStart, targetEnd);

      if (!isAnimating) {
        if (animStart === 0 && animEnd === 0) {
          animStart = targetStart;
          animEnd = targetEnd;
        }
        isAnimating = true;
        renderLineLoop();
      }
    }

    function labelEl(): HTMLElement | null {
      return document.getElementById("toc-current");
    }

    function refreshVisible() {
      var topLimit = 72;
      var bottomLimit = Math.max(topLimit + 120, window.innerHeight - 24);
      visibleHeadingIds = {};
      for (var i = 0; i < headings.length; i++) {
        var rect = headings[i].getBoundingClientRect();
        if (rect.top < bottomLimit && rect.bottom > topLimit) visibleHeadingIds[headings[i].id] = true;
      }
      for (var v = 0; v < links.length; v++) {
        var vid = links[v].getAttribute("data-id") || "";
        links[v].classList.toggle("in-view", !!visibleHeadingIds[vid]);
      }
    }

    function computeActive() {
      if (headings.length === 0) return "";
      var readingLine = (window.pageYOffset || document.documentElement.scrollTop || 0) + 96;
      var current = headings[0];
      for (var i = 0; i < headings.length; i++) {
        var top = headings[i].getBoundingClientRect().top + (window.pageYOffset || document.documentElement.scrollTop || 0);
        if (top <= readingLine) {
          current = headings[i];
        } else {
          break;
        }
      }
      return current ? current.id : "";
    }

    function setActive(id: string) {
      if (!id) {
        updateLineCoords();
        return;
      }
      activeId = id;
      var label = labelEl();
      var firstText = "";
      for (var i = 0; i < links.length; i++) {
        var linkId = links[i].getAttribute("data-id") || "";
        var isActive = linkId === id;
        links[i].classList.toggle("active", isActive);
        if (isActive && label) label.textContent = (links[i].textContent || "").trim();
        if (firstText === "" && links[i].textContent) firstText = links[i].textContent.trim();
      }
      if (label && !label.textContent && firstText) label.textContent = firstText;
      updateLineCoords();
    }

    function scheduleUpdate() {
      if (ticking) return;
      ticking = true;
      window.requestAnimationFrame(function () {
        ticking = false;
        var st = window.pageYOffset || document.documentElement.scrollTop || 0;
        currentDir = st >= lastScrollTop ? "down" : "up";
        lastScrollTop = st <= 0 ? 0 : st;
        refreshVisible();
        setActive(computeActive());
      });
    }

    function onScroll() {
      scheduleUpdate();
    }
    function onResize() {
      scheduleUpdate();
    }
    function onHashChange() {
      if (window.location.hash) {
        var id = decodeURIComponent(window.location.hash.slice(1));
        refreshVisible();
        setActive(id);
      }
    }

    window.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("resize", onResize);
    window.addEventListener("hashchange", onHashChange);

    for (var n = 0; n < links.length; n++) {
      links[n].addEventListener("click", function () {
        if (onNavigate) onNavigate();
        window.setTimeout(scheduleUpdate, 80);
      });
    }

    ensureIndicatorSvg();

    var tocCurrent = labelEl();
    if (tocCurrent) tocCurrent.textContent = (links[0].textContent || "On this page").trim();
    refreshVisible();
    var initial = window.location.hash ? decodeURIComponent(window.location.hash.slice(1)) : computeActive();
    setActive(initial || (headings[0] && headings[0].id));

    if ("IntersectionObserver" in window) {
      var observer = new IntersectionObserver(scheduleUpdate, {
        rootMargin: "-2% 0px -2% 0px",
        threshold: 0,
      });
      for (var k = 0; k < headings.length; k++) {
        observer.observe(headings[k]);
      }
    }

    window.setTimeout(scheduleUpdate, 120);

    onCleanup(function () {
      window.removeEventListener("scroll", onScroll);
      window.removeEventListener("resize", onResize);
      window.removeEventListener("hashchange", onHashChange);
    });
  });

  if (!items || items.length === 0) return <span />;

  return (
    <nav class="toc-nav" ref={navRef}>
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

interface BreadcrumbItem {
  label: string;
  url: string;
  isLast: boolean;
}

interface BreadcrumbsProps {
  items: BreadcrumbItem[];
}

export function Breadcrumbs(props: BreadcrumbsProps) {
  var items = props.items;
  if (!items || items.length === 0) return <span />;

  return (
    <nav class="breadcrumbs">
      {items.map((item, i) => (
        <span>
          {i > 0 && <span class="sep"> / </span>}
          {item.isLast ? (
            <span class="current">{item.label}</span>
          ) : (
            <Link href={item.url}>{item.label}</Link>
          )}
        </span>
      ))}
    </nav>
  );
}

interface PrevNextProps {
  prevTitle?: string;
  prevLink?: string;
  nextTitle?: string;
  nextLink?: string;
}

export function PrevNext(props: PrevNextProps) {
  var prevTitle = props.prevTitle;
  var prevLink = props.prevLink;
  var nextTitle = props.nextTitle;
  var nextLink = props.nextLink;
  const hasPrev = prevLink && prevTitle;
  const hasNext = nextLink && nextTitle;

  if (!hasPrev && !hasNext) return <span />;

  return (
    <div class="docs-nav">
      {hasPrev && (
        <Link className="nav-prev" href={prevLink}>
          <span class="nav-label">Previous</span>
          <span class="nav-title">{prevTitle}</span>
        </Link>
      )}
      {hasNext && (
        <Link className="nav-next" href={nextLink}>
          <span class="nav-label">Next</span>
          <span class="nav-title">{nextTitle}</span>
        </Link>
      )}
    </div>
  );
}

interface SocialLinkItem {
  icon: string;
  url: string;
  name: string;
}

interface SocialLinksProps {
  links: SocialLinkItem[];
}

export function SocialLinks(props: SocialLinksProps) {
  var links = props.links;
  if (!links || links.length === 0) return <span />;

  return (
    <span>
      {links.map((link) => (
        <a class="social-link" href={link.url}>
          <Icon name={link.icon} width="20" height="20" />
        </a>
      ))}
    </span>
  );
}