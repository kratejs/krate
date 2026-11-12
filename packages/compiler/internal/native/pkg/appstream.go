package pkg

import (
	"fmt"
	"strings"
	"time"
)

// yamlQuote renders s as a double-quoted YAML scalar.
func yamlQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", "", "\t", `\t`)
	return `"` + r.Replace(s) + `"`
}

// summaryOf is the one-line description: native.bundle.linux.summary, else the
// product name.
func (m Metadata) summaryOf() string {
	if s := oneLine(m.Summary); s != "" {
		return s
	}
	if s := oneLine(m.Description); s != "" {
		return s
	}
	return oneLine(m.ProductName)
}

// descriptionOf is the long description, falling back to the summary.
func (m Metadata) descriptionOf() string {
	if d := strings.TrimSpace(m.LongDescription); d != "" {
		return d
	}
	return m.summaryOf()
}

// licenseOf is the SPDX expression for AppStream.
func (m Metadata) licenseOf() string {
	if m.License == "" {
		return "LicenseRef-proprietary"
	}
	return m.License
}

// developerOf is the developer name for AppStream.
func (m Metadata) developerOf() string {
	if m.Publisher != "" {
		return publisherDisplay(m.Publisher)
	}
	return m.ProductName
}

// AppStreamMetainfo returns the AppStream metainfo of the app for Flathub and
// software centers. desktopID is the .desktop file name.
func AppStreamMetainfo(m Metadata, desktopID string, date time.Time) []byte {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	w(`<?xml version="1.0" encoding="UTF-8"?>`)
	w(`<component type="desktop-application">`)
	w(`  <id>%s</id>`, xmlEscape(m.AppID))
	w(`  <metadata_license>CC0-1.0</metadata_license>`)
	w(`  <project_license>%s</project_license>`, xmlEscape(m.licenseOf()))
	w(`  <name>%s</name>`, xmlEscape(oneLine(m.ProductName)))
	w(`  <summary>%s</summary>`, xmlEscape(m.summaryOf()))
	w(`  <developer id="%s">`, xmlEscape(m.developerID()))
	w(`    <name>%s</name>`, xmlEscape(oneLine(m.developerOf())))
	w(`  </developer>`)
	w(`  <description>`)
	for _, p := range strings.Split(strings.ReplaceAll(m.descriptionOf(), "\r\n", "\n"), "\n\n") {
		if p = strings.TrimSpace(strings.Join(strings.Fields(p), " ")); p != "" {
			w(`    <p>%s</p>`, xmlEscape(p))
		}
	}
	w(`  </description>`)
	w(`  <launchable type="desktop-id">%s</launchable>`, xmlEscape(desktopID))
	if m.Homepage != "" {
		w(`  <url type="homepage">%s</url>`, xmlEscape(m.Homepage))
	}
	w(`  <provides>`)
	w(`    <id>%s</id>`, xmlEscape(desktopID))
	w(`  </provides>`)
	w(`  <releases>`)
	w(`    <release version="%s" date="%s"/>`, xmlEscape(m.Version), date.UTC().Format("2006-01-02"))
	w(`  </releases>`)
	w(`  <content_rating type="oars-1.1"/>`)
	w(`</component>`)
	return []byte(b.String())
}

// developerID is a reverse-DNS id for the AppStream developer element: the
// first three segments of the app id.
func (m Metadata) developerID() string {
	parts := strings.Split(m.AppID, ".")
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return strings.Join(parts, ".")
}
