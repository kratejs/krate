package config

import (
	"strings"
	"testing"
)

func TestValidateNativeDistribution(t *testing.T) {
	cases := []struct {
		name string
		mut  func(n *NativeConfig)
		want string
	}{
		{"all valid", func(n *NativeConfig) {
			n.Bundle.MacOS = NativeBundleMacOS{Universal: true, MAS: NativeMAS{TeamID: "ABCDE12345", Network: []string{"client", "server"}, Files: "read-only"}}
			n.Bundle.Windows.Store = NativeWindowsStore{IdentityName: "12345Acme.AcmeNotes", PublisherID: "CN=01234567-89AB-CDEF-0123-456789ABCDEF", PublisherDisplayName: "Acme Inc"}
			n.Bundle.Linux = NativeBundleLinux{Formats: []string{"flatpak", "snap", "deb"}, Summary: "Notes", License: "MIT OR Apache-2.0",
				Homepage: "https://acme.example", Flatpak: NativeFlatpak{RuntimeVersion: "48", FinishArgs: []string{"--filesystem=xdg-documents"}},
				Snap: NativeSnap{Grade: "stable", Confinement: "strict"}}
		}, ""},
		{"team id", func(n *NativeConfig) { n.Bundle.MacOS.MAS.TeamID = "abc" }, "bundle.macos.mas.teamId"},
		{"mas network", func(n *NativeConfig) { n.Bundle.MacOS.MAS.Network = []string{"all"} }, "mas.network"},
		{"mas files", func(n *NativeConfig) { n.Bundle.MacOS.MAS.Files = "rw" }, "mas.files"},
		{"store name", func(n *NativeConfig) { n.Bundle.Windows.Store.IdentityName = "a b" }, "store.identityName"},
		{"store publisher", func(n *NativeConfig) { n.Bundle.Windows.Store.PublisherID = "Acme" }, "store.publisherId"},
		{"store display", func(n *NativeConfig) { n.Bundle.Windows.Store.PublisherDisplayName = "A\nB" }, "publisherDisplayName"},
		{"license", func(n *NativeConfig) { n.Bundle.Linux.License = "free as in beer" }, "bundle.linux.license"},
		{"homepage", func(n *NativeConfig) { n.Bundle.Linux.Homepage = "acme.example" }, "bundle.linux.homepage"},
		{"summary newline", func(n *NativeConfig) { n.Bundle.Linux.Summary = "a\nb" }, "bundle.linux.summary"},
		{"flatpak runtime", func(n *NativeConfig) { n.Bundle.Linux.Flatpak.Runtime = "gnome" }, "flatpak.runtime"},
		{"flatpak version", func(n *NativeConfig) { n.Bundle.Linux.Flatpak.RuntimeVersion = "latest" }, "runtimeVersion"},
		{"flatpak arg", func(n *NativeConfig) { n.Bundle.Linux.Flatpak.FinishArgs = []string{"filesystem=home"} }, "finishArgs"},
		{"snap grade", func(n *NativeConfig) { n.Bundle.Linux.Snap.Grade = "beta" }, "snap.grade"},
		{"snap confinement", func(n *NativeConfig) { n.Bundle.Linux.Snap.Confinement = "loose" }, "snap.confinement"},
		{"linux format", func(n *NativeConfig) { n.Bundle.Linux.Formats = []string{"pacman"} }, "flatpak or snap"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			c.Native = &NativeConfig{AppID: "com.acme.app"}
			tc.mut(c.Native)
			_, err := c.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "hint:") {
				t.Errorf("no hint in %v", err)
			}
		})
	}
}
