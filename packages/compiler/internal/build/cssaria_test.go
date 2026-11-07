package build

import (
	"regexp"
	"strings"
	"testing"
)

// idRefsResolve fails when an aria-controls value has no matching id.
func idRefsResolve(t *testing.T, html string) {
	t.Helper()
	ids := map[string]bool{}
	for _, m := range regexp.MustCompile(`\sid=("?)([^"\s>]+)`).FindAllStringSubmatch(html, -1) {
		ids[m[2]] = true
	}
	refs := regexp.MustCompile(`aria-controls=("?)([^"\s>]+)`).FindAllStringSubmatch(html, -1)
	if len(refs) == 0 {
		t.Fatalf("expected aria-controls on the opening triggers:\n%.1500s", html)
	}
	for _, m := range refs {
		if !ids[m[2]] {
			t.Errorf("aria-controls=%q has no matching id", m[2])
		}
	}
}

// TestBuildOverlayARIAAndEnhancer pins the accessible markup of the zero-JS
// overlays: roles on the focusable label triggers, hidden controllers, panel
// roles and ids, and the keyboard enhancer script (no page bundle).
func TestBuildOverlayARIAAndEnhancer(t *testing.T) {
	src := `export default function Page() {
	const dlg = createCSSGroup(null, { as: 'dialog' });
	const menu = createCSSGroup(null, { as: 'menu' });
	const alert = createCSSGroup(null, { as: 'modal', label: 'Confirm' });
	return (
		<div>
			<button {...dlg.trigger('d')}>Open dialog</button>
			<div {...dlg.panel('d')}><h2 data-ka="title">Title</h2><button {...dlg.close()}>Close</button></div>
			<button {...menu.trigger('m')}>Menu</button>
			<div {...menu.panel('m')}><button {...menu.close()}>Item</button></div>
			<button {...alert.trigger('a')}>Delete</button>
			<div {...alert.panel('a')}>Sure?</div>
		</div>
	);
}`
	html, _, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if hasHydrationScript(html) {
		t.Errorf("overlays must stay free of a hydration bundle:\n%.1200s", html)
	}
	if !strings.Contains(html, "__krateAria") {
		t.Errorf("expected the keyboard enhancer to be inlined")
	}
	// The router runs boot scripts after a soft navigation, so the tag must be
	// marked or the enhancer never reaches pages entered client-side.
	if !bootScriptHolds(html, "__krateAria") {
		t.Errorf("the keyboard enhancer script must carry data-krate-boot:\n%.600s", html)
	}
	idRefsResolve(t, html)
	for _, want := range []string{
		`aria-haspopup=dialog`, `aria-haspopup=menu`,
		`role=dialog`, `role=alertdialog`, `role=menu`, `role=menuitem`,
		`aria-modal=true`, `aria-label=Confirm`, `data-ka=p`, `data-kz=dialog`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %s in:\n%.2500s", want, html)
		}
	}
	// Close buttons are plain buttons, not popup triggers.
	if m := regexp.MustCompile(`<label[^>]*data-ka=c[^>]*>`).FindAllString(html, -1); len(m) != 2 {
		t.Errorf("expected two close/item labels, got %v", m)
	} else {
		for _, l := range m {
			if strings.Contains(l, "aria-haspopup") || strings.Contains(l, "aria-expanded") {
				t.Errorf("close/item label must not carry popup state: %s", l)
			}
		}
	}
	// Controllers are out of the tab order and the accessibility tree.
	for _, in := range regexp.MustCompile(`<input[^>]*type=radio[^>]*>`).FindAllString(html, -1) {
		if !strings.Contains(in, "aria-hidden=true") || !strings.Contains(in, "tabindex=-1") {
			t.Errorf("controller input must be aria-hidden and tabindex=-1: %s", in)
		}
	}
	// Menus no longer wrap the whole widget in role=menu.
	if strings.Contains(html, `style=display:contents role=menu`) {
		t.Errorf("role=menu belongs on the panel, not the scope anchor")
	}
}

// TestBuildSwitchRoleOnNativeCheckbox verifies role=switch lands on the
// focusable checkbox (not on a wrapping label) and the control stays focusable.
func TestBuildSwitchRoleOnNativeCheckbox(t *testing.T) {
	src := `export default function Page() {
	const [on, setOn] = createCSSToggle(false, { as: 'switch' });
	return <button type="button" onClick={() => setOn(!on())}><span>Airplane mode</span></button>;
}`
	html, _, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	in := regexp.MustCompile(`<input[^>]*type=checkbox[^>]*>`).FindString(html)
	if !strings.Contains(in, "role=switch") {
		t.Errorf("role=switch must be on the checkbox input: %s", in)
	}
	if strings.Contains(in, "aria-hidden") || strings.Contains(in, "tabindex=-1") {
		t.Errorf("switch input must stay focusable and exposed: %s", in)
	}
	if regexp.MustCompile(`<label[^>]*role=switch`).MatchString(html) {
		t.Errorf("role=switch must not be on the label:\n%.800s", html)
	}
	if strings.Contains(html, "__krateAria") {
		t.Errorf("a native switch needs no enhancer script")
	}
}

// bootScriptHolds reports whether an inline <script data-krate-boot> in html
// contains marker.
func bootScriptHolds(html, marker string) bool {
	for _, m := range regexp.MustCompile(`(?s)<script[^>]*data-krate-boot[^>]*>(.*?)</script>`).FindAllStringSubmatch(html, -1) {
		if strings.Contains(m[1], marker) {
			return true
		}
	}
	return false
}
