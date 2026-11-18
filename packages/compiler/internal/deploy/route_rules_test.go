package deploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func ruleConfig() *config.Config {
	cfg := config.Default()
	cfg.Redirects = []config.Redirect{
		{Source: "/a", Destination: "/b", StatusCode: 308},
		{Source: "/store", Destination: "/templates", StatusCode: 308, Has: []config.RouteCondition{{Type: "query", Key: "vehicle"}}},
	}
	cfg.Headers = []config.HeaderRule{
		{Source: "/h", Headers: map[string]string{"X-A": "1"}, Has: []config.RouteCondition{{Type: "cookie", Key: "c"}}},
	}
	return cfg
}

// A static host cannot test the query, so a conditional rule is skipped
// instead of redirecting every request.
func TestNetlifySkipsConditionalRulesAndKeepsStatusCode(t *testing.T) {
	out := t.TempDir()
	if _, err := Emit("netlify", ruleConfig(), out, out); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(out, "_redirects"))
	got := string(data)
	if !strings.Contains(got, "/a\t/b\t308") {
		t.Errorf("statusCode lost:\n%s", got)
	}
	if strings.Contains(got, "/store\t") || !strings.Contains(got, "# skipped /store") {
		t.Errorf("conditional redirect must be skipped with a note:\n%s", got)
	}
	hdr, _ := os.ReadFile(filepath.Join(out, "_headers"))
	if strings.Contains(string(hdr), "X-A") {
		t.Errorf("conditional header rule emitted unconditionally:\n%s", hdr)
	}
}

func TestVercelCarriesStatusCodeAndConditions(t *testing.T) {
	out := t.TempDir()
	if _, err := Emit("vercel", ruleConfig(), out, out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "vercel.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Redirects []map[string]any `json:"redirects"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Redirects) != 2 {
		t.Fatalf("redirects = %v", doc.Redirects)
	}
	if doc.Redirects[0]["statusCode"] != float64(308) || doc.Redirects[0]["permanent"] != nil {
		t.Errorf("rule 0 = %v", doc.Redirects[0])
	}
	if has, _ := doc.Redirects[1]["has"].([]any); len(has) != 1 {
		t.Errorf("rule 1 lost its has: %v", doc.Redirects[1])
	}
}
