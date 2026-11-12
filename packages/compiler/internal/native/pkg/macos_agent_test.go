package pkg

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestInfoPlistAgent(t *testing.T) {
	m := testMeta(t)
	if strings.Contains(string(InfoPlist(m, "Acme Notes")), "LSUIElement") {
		t.Fatal("a regular app has LSUIElement")
	}
	m.MacAgent = true
	plist := string(InfoPlist(m, "Acme Notes"))
	if !strings.Contains(plist, "<key>LSUIElement</key>\n\t<true/>") {
		t.Fatalf("an agent app lacks LSUIElement:\n%s", plist)
	}
	if strings.Count(plist, "LSUIElement") != 1 || strings.Index(plist, "LSUIElement") > strings.LastIndex(plist, "</dict>") {
		t.Fatalf("LSUIElement is misplaced:\n%s", plist)
	}
}

func TestMetadataAgentFromConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Native = &config.NativeConfig{AppID: "com.acme.menu"}
	cfg.Native.Bundle.MacOS.Agent = true
	if m := MetadataFromConfig(t.TempDir(), cfg); !m.MacAgent {
		t.Fatalf("metadata %+v", m)
	}
}
