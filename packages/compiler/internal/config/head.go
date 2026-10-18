package config

import (
	"bytes"
	"encoding/json"
)

// HeadScript is a script tag injected into every page's <head>. Set either Src
// (external) or Content (inline). Attrs adds arbitrary attributes (e.g. data-*).
type HeadScript struct {
	Src     string            `json:"src,omitempty"`
	Content string            `json:"content,omitempty"`
	Type    string            `json:"type,omitempty"`
	Async   bool              `json:"async,omitempty"`
	Defer   bool              `json:"defer,omitempty"`
	Module  bool              `json:"module,omitempty"`
	Attrs   map[string]string `json:"attrs,omitempty"`
}

// HeadMeta is a <meta> tag injected into every page's <head>.
type HeadMeta struct {
	Name      string `json:"name,omitempty"`
	Property  string `json:"property,omitempty"`
	HTTPEquiv string `json:"httpEquiv,omitempty"`
	Charset   string `json:"charset,omitempty"`
	Content   string `json:"content,omitempty"`
}

// HeadLink is a <link> tag injected into every page's <head>.
type HeadLink struct {
	Rel         string `json:"rel"`
	Href        string `json:"href"`
	As          string `json:"as,omitempty"`
	Type        string `json:"type,omitempty"`
	CrossOrigin string `json:"crossOrigin,omitempty"`
	Media       string `json:"media,omitempty"`
	Sizes       string `json:"sizes,omitempty"`
}

// HeadConfig configures site-wide <head> injection. It also accepts the concise
// array form (`head: ["<script ...>"]`), stored in Raw.
type HeadConfig struct {
	Meta    []HeadMeta   `json:"meta,omitempty"`
	Links   []HeadLink   `json:"links,omitempty"`
	Scripts []HeadScript `json:"scripts,omitempty"`
	Raw     []string     `json:"raw,omitempty"`
}

func (h *HeadConfig) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var raw []string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		h.Raw = raw
		return nil
	}
	type alias HeadConfig
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*h = HeadConfig(a)
	return nil
}
