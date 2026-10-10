package plugin

import (
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/docs"
)

// altLink is one alternate URL for the LocaleSwitcher / VersionSwitcher.
type altLink struct {
	Code  string `json:"code,omitempty"`
	Label string `json:"label"`
	URL   string `json:"url"`
}

// docsLocaleInfo carries the per-page i18n/versioning context into the generated
// page and the theme.
type docsLocaleInfo struct {
	Locale         string    `json:"locale,omitempty"`
	DefaultLocale  string    `json:"defaultLocale,omitempty"`
	Version        string    `json:"version,omitempty"`
	CurrentVersion string    `json:"currentVersion,omitempty"`
	Locales        []altLink `json:"locales,omitempty"`
	Versions       []altLink `json:"versions,omitempty"`
	VersionBanner  bool      `json:"versionBanner,omitempty"`
}

// resolveLocales returns the configured locales (or [""] when i18n is off) and
// the default locale.
func resolveLocales(c config.I18nConfig) (locales []string, def string) {
	if !c.Enabled() {
		return []string{""}, ""
	}
	def = c.DefaultLocale
	if def == "" {
		def = c.Locales[0]
	}
	return c.Locales, def
}

// resolveVersions returns the configured versions (or [""] when versioning is
// off) and the current version.
func resolveVersions(c config.VersionsConfig) (versions []string, cur string) {
	if !c.Enabled() {
		return []string{""}, ""
	}
	cur = c.Current
	if cur == "" {
		cur = c.Versions[0]
	}
	return c.Versions, cur
}

// docsLocaleDir resolves the content directory for a (locale, version) pair:
// non-default locale and/or non-current version live in subdirectories
// (src/content/docs/<locale>/<version>/...).
func docsLocaleDir(base, locale, defaultLocale, version, currentVersion string) string {
	dir := base
	if locale != "" && locale != defaultLocale {
		dir = filepath.Join(dir, locale)
	}
	if version != "" && version != currentVersion {
		dir = filepath.Join(dir, version)
	}
	return dir
}

// excludedSegments lists directory names that belong to other locales/versions
// and must not be scanned as part of the default/current bundle (e.g. the `fr`
// dir when scanning the default `en` content).
func excludedSegments(locales []string, defaultLocale string, versions []string, currentVersion string) map[string]bool {
	ex := map[string]bool{}
	for _, l := range locales {
		if l != "" && l != defaultLocale {
			ex[l] = true
		}
	}
	for _, v := range versions {
		if v != "" && v != currentVersion {
			ex[v] = true
		}
	}
	return ex
}

// filterExcludedPages drops pages whose first path segment is an excluded
// locale/version directory.
func filterExcludedPages(ps []docs.Page, ex map[string]bool) []docs.Page {
	if len(ex) == 0 {
		return ps
	}
	out := ps[:0]
	for _, p := range ps {
		seg := p.Path
		if i := strings.IndexByte(seg, '/'); i >= 0 {
			seg = seg[:i]
		}
		if ex[seg] {
			continue
		}
		out = append(out, p)
	}
	return out
}

// docsURL builds a docs page URL for a (locale, version): the default locale is
// unprefixed; the current version is unprefixed. Locale prefixes the whole URL
// and version sits under /docs.
//
//	en/current: /docs/guide/
//	fr/current: /fr/docs/guide/
//	en/v1:      /docs/v1/guide/
//	fr/v1:      /fr/docs/v1/guide/
func docsURL(locale, defaultLocale, version, currentVersion, path string) string {
	norm := docs.NormalizePagePath(path)
	u := "/docs/"
	if version != "" && version != currentVersion {
		u += version + "/"
	}
	if norm != "" {
		u += norm + "/"
	}
	if locale != "" && locale != defaultLocale {
		u = "/" + locale + u
	}
	return u
}

// docsRoute is the output route (built directory) for a (locale, version, path).
func docsRoute(locale, defaultLocale, version, currentVersion, path string) string {
	norm := docs.NormalizePagePath(path)
	route := "docs"
	if version != "" && version != currentVersion {
		route += "/" + version
	}
	if norm != "" {
		route += "/" + norm
	}
	if locale != "" && locale != defaultLocale {
		route = locale + "/" + route
	}
	return route
}

// localeSwitchLinks builds the LocaleSwitcher entries for a page from a
// translation map (locale -> URL), falling back to each locale's docs root.
func localeSwitchLinks(locales []string, defaultLocale string, byLocale map[string]string) []altLink {
	out := make([]altLink, 0, len(locales))
	for _, loc := range locales {
		url := byLocale[loc]
		if url == "" {
			url = docs.LocaleURL(loc, defaultLocale, "index")
		}
		out = append(out, altLink{Code: loc, Label: localeLabel(loc), URL: url})
	}
	return out
}

// versionSwitchLinks builds the VersionSwitcher entries for a page. Each entry
// links to the SAME page in the target version when it exists; otherwise it
// falls back to that version's root (its first page), so switching never lands
// on the current version's page (the previous hardcoded "/docs/" fallback).
func versionSwitchLinks(versions []string, currentVersion, locale, defaultLocale string, byVersion, rootByVersion map[string]string) []altLink {
	out := make([]altLink, 0, len(versions))
	for _, ver := range versions {
		url := byVersion[ver]
		if url == "" {
			url = rootByVersion[locale+"\x00"+ver]
		}
		// A locale may not translate every version; fall back to the default
		// locale's root for that version rather than a 404 (/fr/docs/v1/).
		if url == "" && locale != defaultLocale {
			url = rootByVersion[defaultLocale+"\x00"+ver]
		}
		if url == "" {
			url = docsURL(locale, defaultLocale, ver, currentVersion, "index")
		}
		out = append(out, altLink{Code: ver, Label: ver, URL: url})
	}
	return out
}

// firstSidebarURL returns the URL of the first navigable page in a sidebar tree
// (depth-first), which acts as the version's landing page when no index exists.
func firstSidebarURL(items []docs.SidebarItem) string {
	for _, it := range items {
		if it.URL != "" {
			return it.URL
		}
		if it.IndexURL != "" {
			return it.IndexURL
		}
		if u := firstSidebarURL(it.Children); u != "" {
			return u
		}
	}
	return ""
}

// localeLabel maps a locale code to a display label, using the locale's own
// endonym when known.
func localeLabel(code string) string {
	switch strings.ToLower(code) {
	case "en":
		return "English"
	case "fr":
		return "Français"
	case "de":
		return "Deutsch"
	case "es":
		return "Español"
	case "it":
		return "Italiano"
	case "pt":
		return "Português"
	case "ja":
		return "日本語"
	case "ko":
		return "한국어"
	case "zh", "zh-cn":
		return "中文"
	case "ru":
		return "Русский"
	case "nl":
		return "Nederlands"
	case "pl":
		return "Polski"
	case "tr":
		return "Türkçe"
	case "ar":
		return "العربية"
	case "hi":
		return "हिन्दी"
	default:
		if code == "" {
			return ""
		}
		return strings.ToUpper(code[:1]) + code[1:]
	}
}
