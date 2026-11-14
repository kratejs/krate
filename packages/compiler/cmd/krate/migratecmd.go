package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/build"
	"github.com/kratejs/krate/packages/compiler/internal/jseval"
	"github.com/kratejs/krate/packages/compiler/internal/migrate"
)

func runMigrate(flags cliFlags, args []string) {
	if len(args) < 2 {
		printCommandUsage(os.Stderr, "migrate")
		os.Exit(1)
	}
	switch args[1] {
	case "nextjs":
		runMigrateNext(flags, args)
	case "rollback":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: krate migrate rollback <backup-dir>")
			os.Exit(1)
		}
		if err := migrate.Rollback(args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "%sRollback error:%s %v\n", cRed, cReset, err)
			os.Exit(1)
		}
		fmt.Printf("migrate: rolled back %s\n", args[2])
	default:
		fmt.Fprintf(os.Stderr, "%sUnknown migration:%s %s\n", cRed, cReset, args[1])
		fmt.Fprintln(os.Stderr, "Supported: nextjs, rollback")
		os.Exit(1)
	}
}

func runMigrateNext(flags cliFlags, args []string) {
	root := "."
	if len(args) > 2 {
		root = args[2]
	}
	if abs, err := filepath.Abs(root); err == nil {
		requireConfigFlag(abs, flags)
	}
	rep, err := migrate.ScanNextWithOptions(root, migrate.ScanOptions{UpdatePackage: flags.UpdatePackage, KrateVersion: version})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sMigration plan error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}
	if flags.Report != "" {
		if err := writeMigrationReport(flags.Report, rep); err != nil {
			fmt.Fprintf(os.Stderr, "%sReport error:%s %v\n", cRed, cReset, err)
			os.Exit(1)
		}
		if !flags.JSON {
			fmt.Printf("migration report: %s\n", flags.Report)
		}
	}

	if !flags.Apply {
		if flags.JSON {
			writeJSON(rep)
			return
		}
		printMigrationPlan(rep)
		if len(rep.Files) > 0 {
			if n := overwriteCount(rep); n > 0 {
				fmt.Printf("\nDry run. %d file(s) above already exist, so --apply needs --force.\nThe originals are backed up first and `krate migrate rollback` restores them.\n", n)
			} else {
				fmt.Println("\nDry run. Re-run with --apply to write these files.")
			}
		}
		return
	}

	if flags.Verify {
		if missing := unresolvedKratePackages(rep); len(missing) > 0 {
			fmt.Fprintf(os.Stderr, "%sCannot verify:%s %s does not resolve from %s, so nothing was written.\n", cRed, cReset, strings.Join(missing, ", "), rep.Root)
			fmt.Fprintln(os.Stderr, "  Install the dependencies first: run with --update-package and install (npm/pnpm/yarn/bun install), then re-run with --apply --verify.")
			fmt.Fprintln(os.Stderr, "  Or run --apply without --verify and run `krate build` once the packages are installed.")
			os.Exit(1)
		}
	}

	res, err := migrate.Apply(rep, migrate.ApplyOptions{Force: flags.Force})
	if err != nil {
		if flags.JSON {
			fmt.Fprintf(os.Stderr, "%sMigration error:%s %v\n", cRed, cReset, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "%sMigration error:%s %v\n", cRed, cReset, err)
		if len(rep.Manual) > 0 {
			printMigrationManual(rep)
		}
		os.Exit(1)
	}

	if flags.Verify {
		ver, verr := build.VerifyMigration(root, rep.Routes, build.MigrationVerifyOptions{})
		if verr != nil {
			fmt.Fprintf(os.Stderr, "%sVerification error:%s %v\n", cRed, cReset, verr)
			os.Exit(1)
		}
		if flags.JSON {
			writeJSON(struct {
				Plan         *migrate.Report              `json:"plan"`
				Result       *migrate.ApplyResult         `json:"result"`
				Verification *build.MigrationVerification `json:"verification"`
			}{Plan: rep, Result: res, Verification: ver})
			if !ver.OK() {
				os.Exit(1)
			}
			return
		}
		out, ok := build.FormatMigrationVerification(ver)
		fmt.Print(out)
		if !ok {
			fmt.Fprintf(os.Stderr, "\nVerification failed. Keep the backup or roll back with:\n  krate migrate rollback %s\n", res.BackupDir)
			os.Exit(1)
		}
	}

	if flags.JSON {
		writeJSON(struct {
			Plan   *migrate.Report      `json:"plan"`
			Result *migrate.ApplyResult `json:"result"`
		}{Plan: rep, Result: res})
		return
	}

	fmt.Printf("migrate: wrote %d file(s) from %s\n", len(res.Written), rep.Root)
	fmt.Printf("  backup: %s\n", res.BackupDir)
	if len(res.Moved) > 0 {
		fmt.Printf("  moved aside into the backup: %s\n", strings.Join(res.Moved, ", "))
	}
	fmt.Printf("  rollback: krate migrate rollback %s\n", res.BackupDir)
	if len(rep.Manual) > 0 {
		printMigrationManual(rep)
	}
}

// unresolvedKratePackages returns the @krate/* packages that verification will
// need (the config's @krate/core plus any other package a generated file
// imports) and that do not resolve from the project.
func unresolvedKratePackages(rep *migrate.Report) []string {
	need := map[string]bool{"@krate/core": true}
	for _, f := range rep.Files {
		for _, pkg := range []string{"@krate/runtime", "@krate/components"} {
			if bytes.Contains(f.Content, []byte(pkg)) {
				need[pkg] = true
			}
		}
	}
	var missing []string
	for pkg := range need {
		if _, err := jseval.ResolveKratePackage(rep.Root, pkg); err != nil {
			missing = append(missing, pkg)
		}
	}
	sort.Strings(missing)
	return missing
}

func printMigrationPlan(rep *migrate.Report) {
	fmt.Printf("Next.js migration plan: %s\n", rep.Root)
	if len(rep.Detected) > 0 {
		fmt.Printf("  detected: %v\n", rep.Detected)
	}
	fmt.Printf("  %d file(s), %d manual item(s)\n", len(rep.Files), len(rep.Manual))
	for _, f := range rep.Files {
		marker := "+"
		if f.Conflict {
			marker = "!"
		}
		src := f.Source
		if src == "" {
			src = "(generated)"
		}
		note := ""
		if f.Overwrites {
			note = "  (exists: needs --force, backup is made)"
		}
		fmt.Printf("  %s %s -> %s%s\n", marker, src, f.Target, note)
		for _, r := range f.Rewrites {
			fmt.Printf("      %s\n", r)
		}
	}
	for _, w := range rep.Warnings {
		fmt.Printf("  warning: %s\n", w)
	}
	if len(rep.MoveAside) > 0 {
		fmt.Printf("\n--apply moves the original Next route tree(s) into the backup so Krate does not build them twice: %s\n  `krate migrate rollback` moves them back.\n", strings.Join(rep.MoveAside, ", "))
	}
	printMigrationManual(rep)
}

// overwriteCount is the number of planned files whose target already exists.
func overwriteCount(rep *migrate.Report) int {
	n := 0
	for _, f := range rep.Files {
		if f.Overwrites {
			n++
		}
	}
	return n
}

func printMigrationManual(rep *migrate.Report) {
	if len(rep.Manual) == 0 {
		return
	}
	fmt.Println("\nManual follow-up:")
	for _, m := range rep.Manual {
		src := m.Source
		if src == "" {
			src = rep.Root
		}
		fmt.Printf("  ! %s: %s\n", src, m.Reason)
		if m.Hint != "" {
			fmt.Printf("      hint: %s\n", m.Hint)
		}
	}
}

func writeMigrationReport(path string, rep *migrate.Report) error {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return err
	}
	return os.WriteFile(abs, append(data, '\n'), 0644)
}

func writeJSON(v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sJSON error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}
