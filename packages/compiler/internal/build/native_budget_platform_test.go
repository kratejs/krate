package build

import (
	"strings"
	"testing"
)

func TestBudgetVerdictPerPlatform(t *testing.T) {
	section := map[string]map[string]budgetEntry{
		"linux-amd64":  {"shell": {Measured: 100, Max: 125}},
		"darwin-arm64": {"shell": {Measured: 200, Max: 250}},
	}
	cases := []struct {
		plat, name string
		value      int64
		scale      float64
		want       budgetStatus
	}{
		{"linux-amd64", "shell", 125, 1, budgetOK},
		{"linux-amd64", "shell", 126, 1, budgetOver},
		{"linux-amd64", "other", 1, 1, budgetMissing},
		{"darwin-arm64", "shell", 250, 1, budgetOK},
		{"darwin-arm64", "shell", 251, 1, budgetOver},
		{"darwin-arm64", "shell", 400, 2, budgetOK},
		{"darwin-arm64", "other", 1, 1, budgetMissing},
		{"windows-amd64", "shell", 1 << 40, 1, budgetReportOnly},
		{"linux-arm64", "shell", 1 << 40, 1, budgetReportOnly},
	}
	for _, c := range cases {
		if _, _, got := budgetVerdict(section, c.plat, c.name, c.value, c.scale); got != c.want {
			t.Errorf("%s %s=%d: status %d, want %d", c.plat, c.name, c.value, got, c.want)
		}
	}
	// A strict platform without any entry must still fail, not report.
	if _, _, got := budgetVerdict(map[string]map[string]budgetEntry{}, "linux-amd64", "shell", 1, 1); got != budgetMissing {
		t.Errorf("linux-amd64 with no entries: status %d, want missing", got)
	}
}

func TestEvalMemoryEnforcesRecordedEntries(t *testing.T) {
	entries := map[string]budgetEntry{
		"appMinusFloor.total.pss": {Measured: 10, Max: 20},
		"app.total.pss":           {Measured: 100, Max: 120},
		"appMinusFloor.gone.pss":  {Measured: 5, Max: 10},
	}
	res := evalMemory(entries, map[string]int64{"appMinusFloor.total.pss": 21, "app.total.pss": 500})
	got := map[string]memResult{}
	for _, r := range res {
		got[r.name] = r
	}
	if r := got["appMinusFloor.total.pss"]; !r.over || !r.enforced {
		t.Errorf("appMinusFloor.total.pss over its limit must be an enforced failure: %+v", r)
	}
	if r := got["app.total.pss"]; !r.over || r.enforced {
		t.Errorf("absolute values only warn: %+v", r)
	}
	if r := got["appMinusFloor.gone.pss"]; r.over {
		t.Errorf("a role that is gone counts 0 and is not over: %+v", r)
	}
}

func TestBudgetRecordTextIsPasteable(t *testing.T) {
	budgetRecorded.Lock()
	budgetRecorded.sizes, budgetRecorded.startup, budgetRecorded.memory = nil, nil, nil
	budgetRecorded.Unlock()
	t.Cleanup(func() {
		budgetRecorded.Lock()
		budgetRecorded.sizes, budgetRecorded.startup, budgetRecorded.memory = nil, nil, nil
		budgetRecorded.Unlock()
	})
	if budgetRecordText("windows-amd64") != "" {
		t.Fatal("nothing recorded must print nothing")
	}
	budgetRecord("sizes", "shell", budgetEntry{Measured: 8, Max: 10})
	budgetRecord("memory", "appMinusFloor.total.pss", budgetEntry{Measured: 1, Max: 2})
	txt := budgetRecordText("windows-amd64")
	for _, want := range []string{"BUDGET-RECORD-BEGIN windows-amd64\n", "\nBUDGET-RECORD-END windows-amd64\n", `"windows-amd64": {`, `"shell": {`, `"measured": 8`} {
		if !strings.Contains(txt, want) {
			t.Errorf("record text lacks %q:\n%s", want, txt)
		}
	}
}
