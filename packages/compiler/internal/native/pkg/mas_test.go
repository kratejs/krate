package pkg

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func masMeta(t *testing.T) Metadata {
	m := testMeta(t)
	m.MAS = config.NativeMAS{TeamID: "ABCDE12345"}
	m.Permissions = []string{"net", "fs", "notify"}
	return m
}

func entKeys(es []plistEntry) map[string]string {
	m := map[string]string{}
	for _, e := range es {
		m[e.Key] = e.Str
	}
	return m
}

func TestMASEntitlementsFollowPermissions(t *testing.T) {
	m := masMeta(t)
	m.MediaUses = []string{"camera", "microphone", "location"}
	k := entKeys(masEntitlements(m))
	for _, want := range []string{
		"com.apple.security.app-sandbox", "com.apple.security.network.client", "com.apple.security.files.user-selected.read-write",
		"com.apple.security.device.camera", "com.apple.security.device.audio-input", "com.apple.security.personal-information.location",
		"com.apple.application-identifier", "com.apple.developer.team-identifier",
	} {
		if _, ok := k[want]; !ok {
			t.Errorf("missing %s in %v", want, k)
		}
	}
	if k["com.apple.application-identifier"] != "ABCDE12345.com.acme.notes" || k["com.apple.developer.team-identifier"] != "ABCDE12345" {
		t.Errorf("identifiers = %v", k)
	}
	if _, ok := k["com.apple.security.cs.allow-jit"]; ok {
		t.Error("no JIT without a JavaScript host")
	}
	m.HostRuntimes = []string{"node"}
	m.MAS.Network = []string{"client", "server"}
	m.MAS.Files = "read-only"
	k = entKeys(masEntitlements(m))
	if _, ok := k["com.apple.security.cs.allow-jit"]; !ok {
		t.Error("a Node host needs allow-jit")
	}
	if _, ok := k["com.apple.security.network.server"]; !ok {
		t.Error("network.server missing")
	}
	if _, ok := k["com.apple.security.files.user-selected.read-only"]; !ok {
		t.Error("read-only missing")
	}
	if _, ok := k["com.apple.security.files.user-selected.read-write"]; ok {
		t.Error("read-write must not be set with read-only")
	}
	m.MAS.Files = "none"
	for key := range entKeys(masEntitlements(m)) {
		if strings.Contains(key, "user-selected") {
			t.Error("files none still grants " + key)
		}
	}
}
