package jsruntime

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

// The compatibility ledger (compat.json) lists every Web API global, static
// member and Node global the embedded runtime supports, partially supports or
// lacks. Builds scan route sources against it (ScanCompat), the docs table is
// generated from it, and TestCompatLedger keeps it honest.

//go:embed compat.json
var compatJSON []byte

// CompatStatus is how well the embedded runtime provides an API.
type CompatStatus string

const (
	CompatSupported    CompatStatus = "supported"
	CompatPartial      CompatStatus = "partial"
	CompatNotSupported CompatStatus = "not-supported"
	// CompatByDesign marks APIs handled another way on purpose (node: imports
	// route the API route to the Node sidecar).
	CompatByDesign CompatStatus = "by-design"
)

// CompatRow is one ledger entry.
type CompatRow struct {
	// API is a global path such as "TransformStream", "crypto.subtle.encrypt"
	// or "ReadableStream.prototype.tee", or "node:*" for Node builtin modules.
	API     string       `json:"api"`
	Status  CompatStatus `json:"status"`
	Feature string       `json:"feature"`
	Notes   string       `json:"notes,omitempty"`
	// Hint says what to do instead; required for not-supported rows.
	Hint string `json:"hint,omitempty"`
	// Tests names the Go tests (Func or Func/subtest) that verify the row.
	Tests []string `json:"tests,omitempty"`
}

// CompatFeature describes one feature group: the prelude module that provides
// its APIs, the groups that module needs installed before it, and the globals
// it defines (each loads the module on first use in a lazily set up VM).
type CompatFeature struct {
	Title       string   `json:"title"`
	Section     string   `json:"section"`
	Description string   `json:"description"`
	Requires    []string `json:"requires"`
	Globals     []string `json:"globals,omitempty"`
}

// CompatLedger is the parsed compat.json.
type CompatLedger struct {
	Denominator string                   `json:"denominator"`
	Features    map[string]CompatFeature `json:"features"`
	Rows        []CompatRow              `json:"rows"`

	byAPI map[string]int
}

var compatLedger = sync.OnceValues(func() (*CompatLedger, error) {
	return parseCompatLedger(compatJSON)
})

func parseCompatLedger(data []byte) (*CompatLedger, error) {
	var l CompatLedger
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("compat.json: %w", err)
	}
	l.byAPI = make(map[string]int, len(l.Rows))
	for i, r := range l.Rows {
		if _, dup := l.byAPI[r.API]; dup {
			return nil, fmt.Errorf("compat.json: duplicate api %q", r.API)
		}
		l.byAPI[r.API] = i
	}
	return &l, nil
}

// Compat returns the embedded compatibility ledger. The ledger is validated by
// tests, so a parse failure is a programming error.
func Compat() *CompatLedger {
	l, err := compatLedger()
	if err != nil {
		panic(err)
	}
	return l
}

// Lookup returns the row for api.
func (l *CompatLedger) Lookup(api string) (CompatRow, bool) {
	i, ok := l.byAPI[api]
	if !ok {
		return CompatRow{}, false
	}
	return l.Rows[i], true
}
