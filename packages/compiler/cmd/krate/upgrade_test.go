package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.5.1", "0.5.0", 1}, {"0.5.0", "0.5.0", 0}, {"0.4.9", "0.5.0", -1},
		{"1.0.0", "1.0.0-beta.1", 1}, {"1.0.0-alpha", "1.0.0-beta", -1}, {"v0.10.0", "0.9.0", 1},
	}
	for _, c := range cases {
		got, ok := compareSemver(c.a, c.b)
		if !ok || got != c.want {
			t.Errorf("compareSemver(%q, %q) = %d, %v; want %d", c.a, c.b, got, ok, c.want)
		}
	}
	if _, ok := compareSemver("dev", "0.5.0"); ok {
		t.Error("a dev build is not comparable")
	}
}

func TestParseUpgradeTarget(t *testing.T) {
	for in, want := range map[string]string{"": "latest", "latest": "latest", "next": "next", "0.5.1": "0.5.1", "v0.5.1": "0.5.1", "1.0.0-beta.2": "1.0.0-beta.2"} {
		got, err := parseUpgradeTarget(in)
		if err != nil || got != want {
			t.Errorf("parseUpgradeTarget(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"0.5", "1.2.3.4", "Latest!", "../x", "@krate/core"} {
		if _, err := parseUpgradeTarget(in); err == nil {
			t.Errorf("parseUpgradeTarget(%q) accepted", in)
		}
	}
}

func TestProjectUpgradeCommands(t *testing.T) {
	deps := []upgradeDep{{Name: "@krate/core", Dev: true}, {Name: "@krate/runtime"}, {Name: "@krate/components"}}
	got := projectUpgradeCommands("pnpm", "0.5.1", deps, true)
	if len(got) != 2 {
		t.Fatalf("want a prod and a dev command, got %v", got)
	}
	if !reflect.DeepEqual(got[0].Args, []string{"add", "-w", "@krate/runtime@0.5.1", "@krate/components@0.5.1"}) {
		t.Errorf("prod args = %v", got[0].Args)
	}
	if !reflect.DeepEqual(got[1].Args, []string{"add", "-D", "-w", "@krate/core@0.5.1"}) {
		t.Errorf("dev args = %v", got[1].Args)
	}
	if npm := projectUpgradeCommands("npm", "0.5.1", deps[:1], false); npm[0].String() != "npm install --save-dev @krate/core@0.5.1" {
		t.Errorf("npm command = %q", npm[0])
	}
	if bun := projectUpgradeCommands("bun", "0.5.1", deps[:1], false); bun[0].String() != "bun add -d @krate/core@0.5.1" {
		t.Errorf("bun command = %q", bun[0])
	}
}

func TestGlobalUpgradeCommand(t *testing.T) {
	for pm, want := range map[string]string{
		"npm":  "npm install -g @krate/core@0.5.1",
		"pnpm": "pnpm add -g @krate/core@0.5.1",
		"yarn": "yarn global add @krate/core@0.5.1",
		"bun":  "bun add -g @krate/core@0.5.1",
	} {
		if got := globalUpgradeCommand(pm, "0.5.1").String(); got != want {
			t.Errorf("%s: %q, want %q", pm, got, want)
		}
	}
}

func upgradeFixture(t *testing.T) string {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "package.json"), `{"dependencies":{"@krate/runtime":"^0.4.0","react":"^19"},"devDependencies":{"@krate/core":"^0.4.0"}}`)
	writeTestFile(t, filepath.Join(root, "node_modules", "@krate", "core", "package.json"), `{"name":"@krate/core","version":"0.4.2"}`)
	writeTestFile(t, filepath.Join(root, "node_modules", "@krate", "runtime", "package.json"), `{"name":"@krate/runtime","version":"0.4.2"}`)
	return root
}

func TestExecuteUpgradeResolvesTagAndRuns(t *testing.T) {
	root := upgradeFixture(t)
	var ran []string
	var out bytes.Buffer
	err := executeUpgrade(&out, upgradeOptions{
		Root: root, Target: "latest", PM: "npm",
		Lookup: func(tag string) (string, error) { return "0.5.1", nil },
		Run:    func(dir string, c upgradeCommand) error { ran = append(ran, c.String()); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ran) != 2 || ran[0] != "npm install --save-prod @krate/runtime@0.5.1" || ran[1] != "npm install --save-dev @krate/core@0.5.1" {
		t.Errorf("commands = %v", ran)
	}
	for _, want := range []string{"latest resolves to v0.5.1", "0.4.2 -> 0.5.1"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output misses %q:\n%s", want, out.String())
		}
	}
}

func TestExecuteUpgradeDryRunAndUpToDate(t *testing.T) {
	root := upgradeFixture(t)
	ran := 0
	run := func(string, upgradeCommand) error { ran++; return nil }
	var out bytes.Buffer
	if err := executeUpgrade(&out, upgradeOptions{Root: root, Target: "0.5.1", PM: "pnpm", DryRun: true, Run: run}); err != nil {
		t.Fatal(err)
	}
	if ran != 0 || !strings.Contains(out.String(), "Dry run") {
		t.Errorf("dry run executed %d commands:\n%s", ran, out.String())
	}
	out.Reset()
	if err := executeUpgrade(&out, upgradeOptions{Root: root, Target: "0.4.2", PM: "pnpm", Run: run}); err != nil {
		t.Fatal(err)
	}
	if ran != 0 || !strings.Contains(out.String(), "already at v0.4.2") {
		t.Errorf("up-to-date upgrade ran commands:\n%s", out.String())
	}
}

func TestExecuteUpgradeErrors(t *testing.T) {
	root := upgradeFixture(t)
	failLookup := func(string) (string, error) { return "", errors.New("offline") }
	if err := executeUpgrade(&bytes.Buffer{}, upgradeOptions{Root: root, Target: "latest", PM: "npm", Lookup: failLookup}); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Errorf("lookup failure not reported: %v", err)
	}
	empty := t.TempDir()
	writeTestFile(t, filepath.Join(empty, "package.json"), `{"dependencies":{"react":"^19"}}`)
	if err := executeUpgrade(&bytes.Buffer{}, upgradeOptions{Root: empty, Target: "0.5.1", PM: "npm"}); err == nil || !strings.Contains(err.Error(), "--global") {
		t.Errorf("a project without @krate packages should point at --global: %v", err)
	}
	runFail := func(string, upgradeCommand) error { return errors.New("boom") }
	if err := executeUpgrade(&bytes.Buffer{}, upgradeOptions{Root: root, Target: "0.5.1", PM: "npm", Run: runFail}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("run failure not reported: %v", err)
	}
}

func TestExecuteUpgradeGlobal(t *testing.T) {
	var ran []string
	err := executeUpgrade(&bytes.Buffer{}, upgradeOptions{
		Root: t.TempDir(), Target: "0.5.1", Global: true, PM: "npm",
		Run: func(dir string, c upgradeCommand) error { ran = append(ran, c.String()); return nil },
	})
	if err != nil || len(ran) != 1 || ran[0] != "npm install -g @krate/core@0.5.1" {
		t.Errorf("global upgrade ran %v, err %v", ran, err)
	}
}
