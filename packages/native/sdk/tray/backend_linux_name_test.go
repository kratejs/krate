//go:build linux

package tray

import "testing"

func TestItemBusName(t *testing.T) {
	if got := itemBusName("", 42); got != "org.kde.StatusNotifierItem-42-1" {
		t.Errorf("direct = %s", got)
	}
	if got := itemBusName("com.acme.App", 2); got != "com.acme.App.StatusNotifierItem" {
		t.Errorf("flatpak = %s", got)
	}
}
