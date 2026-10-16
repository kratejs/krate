package check

import "testing"

func TestDuplicateID(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<h1 id="x">A</h1><p id="x">B</p>`, "")}
	if fs := runOne(t, page, cfg); !hasRule(fs, "a11y/duplicate-id") {
		t.Fatalf("expected duplicate-id finding, got %+v", fs)
	}
}

func TestDuplicateIDClean(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<h1 id="a">A</h1><p id="b">B</p>`, "")}
	if fs := runOne(t, page, cfg); hasRule(fs, "a11y/duplicate-id") {
		t.Fatalf("unexpected duplicate-id finding: %+v", fs)
	}
}

func TestLandmarkMissing(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<div><h1>Title</h1><p>Body</p></div>`, "")}
	if fs := runOne(t, page, cfg); !hasRule(fs, "a11y/landmark") {
		t.Fatalf("expected landmark finding, got %+v", fs)
	}
}

func TestLandmarkMainClean(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<main><h1>Title</h1></main>`, "")}
	if fs := runOne(t, page, cfg); hasRule(fs, "a11y/landmark") {
		t.Fatalf("unexpected landmark finding: %+v", fs)
	}
}

func TestFormLabelMissing(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<main><h1>T</h1><input type="text"></main>`, "")}
	if fs := runOne(t, page, cfg); !hasRule(fs, "a11y/form-label") {
		t.Fatalf("expected form-label finding, got %+v", fs)
	}
}

func TestFormLabelPresent(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<main><h1>T</h1><label for="n">Name</label><input id="n" type="text"></main>`, "")}
	if fs := runOne(t, page, cfg); hasRule(fs, "a11y/form-label") {
		t.Fatalf("unexpected form-label finding: %+v", fs)
	}
}

func TestFormLabelHiddenAndButtonSkipped(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<main><h1>T</h1><input type="hidden" name="x"><input type="submit" value="Go"><input type="text" aria-label="Search"></main>`, "")}
	if fs := runOne(t, page, cfg); hasRule(fs, "a11y/form-label") {
		t.Fatalf("hidden/submit/aria-labelled controls should not be flagged: %+v", fs)
	}
}

func TestPositiveTabindex(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<main><h1>T</h1><a href="/x" tabindex="3">x</a></main>`, "")}
	if fs := runOne(t, page, cfg); !hasRule(fs, "a11y/tabindex") {
		t.Fatalf("expected tabindex finding, got %+v", fs)
	}
}

func TestTabindexZeroAllowed(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<main><h1>T</h1><a href="/x" tabindex="0">x</a></main>`, "")}
	if fs := runOne(t, page, cfg); hasRule(fs, "a11y/tabindex") {
		t.Fatalf("tabindex=0 should not be flagged: %+v", fs)
	}
}

func TestARIARoleUnknown(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<main><h1>T</h1><div role="buton">x</div></main>`, "")}
	if fs := runOne(t, page, cfg); !hasRule(fs, "a11y/aria-role") {
		t.Fatalf("expected aria-role finding, got %+v", fs)
	}
}

func TestARIARoleKnownClean(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<main><h1>T</h1><div role="navigation">x</div></main>`, "")}
	if fs := runOne(t, page, cfg); hasRule(fs, "a11y/aria-role") {
		t.Fatalf("valid role should not be flagged: %+v", fs)
	}
}

func TestColorContrastLow(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<main><h1>T</h1><p style="color:#777;background-color:#888">faint</p></main>`, "")}
	if fs := runOne(t, page, cfg); !hasRule(fs, "a11y/color-contrast") {
		t.Fatalf("expected color-contrast finding, got %+v", fs)
	}
}

func TestColorContrastOK(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<main><h1>T</h1><p style="color:#000;background-color:#fff">clear</p></main>`, "")}
	if fs := runOne(t, page, cfg); hasRule(fs, "a11y/color-contrast") {
		t.Fatalf("high contrast should not be flagged: %+v", fs)
	}
}
