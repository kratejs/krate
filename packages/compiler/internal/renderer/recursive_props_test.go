package renderer

import (
	"strings"
	"testing"
)

// A recursive component (the docs sidebar shape) must resolve each
// instance's props independently. The pre-hardening compiler leaked a child
// instance's `item` binding into the parent, so a section without a badge
// rendered its last child's badge. The section badge must appear only when
// the section item itself has one.
func TestRecursiveComponentPropsDoNotLeakBetweenInstances(t *testing.T) {
	const section = `function SidebarSection(props) {
  var item = props.item;
  const hasChildren = item.children && item.children.length > 0;
  if (!hasChildren && item.url) {
    return (
      <Link className="sidebar-link" href={item.url}>
        <span class="sidebar-label">{item.title}</span>
        {item.badge && <span class="sidebar-badge">{item.badge.text}</span>}
      </Link>
    );
  }
  return (
    <div class="sec">
      {item.indexURL ? (
        <Link className="sidebar-section-link" href={item.indexURL}>
          <span class="sidebar-label">{item.title}</span>
          {item.badge && <span class="sidebar-badge">{item.badge.text}</span>}
        </Link>
      ) : (
        <span class="sidebar-section-link"><span class="sidebar-label">{item.title}</span></span>
      )}
      <div class="kids">
        {item.children && item.children.map((child) => (
          <SidebarSection item={child} />
        ))}
      </div>
    </div>
  );
}
`
	cases := []struct {
		name, items, want string
		absent             []string
	}{
		{
			name:  "child badge only",
			items: `[{title: "Features", indexURL: "/f/", children: [{title: "Leaf", url: "/f/l/", badge: {text: "Beta"}}]}]`,
			want:  `<span class="sidebar-label">Leaf</span><span class="sidebar-badge">Beta</span>`,
			absent: []string{
				`<span class="sidebar-label">Features</span><span class="sidebar-badge">`,
			},
		},
		{
			name:  "section badge only",
			items: `[{title: "Features", indexURL: "/f/", badge: {text: "New"}, children: [{title: "Leaf", url: "/f/l/"}]}]`,
			want:  `<span class="sidebar-label">Features</span><span class="sidebar-badge">New</span>`,
			absent: []string{
				`<span class="sidebar-label">Leaf</span><span class="sidebar-badge">`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := section + `export default function Page() {
  const items = ` + tc.items + `;
  return <nav>{items.map((it) => <SidebarSection item={it} />)}</nav>;
}`
			res, _ := fullPipeline(t, src)
			if !strings.Contains(res.HTML, tc.want) {
				t.Errorf("missing %q:\n%s", tc.want, res.HTML)
			}
			for _, a := range tc.absent {
				if strings.Contains(res.HTML, a) {
					t.Errorf("badge leaked between recursive instances (%q):\n%s", a, res.HTML)
				}
			}
		})
	}
}
