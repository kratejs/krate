package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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
	rep, err := migrate.ScanNextWithOptions(root, migrate.ScanOptions{UpdatePackage: flags.UpdatePackage})
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
			fmt.Println("\nDry run. Re-run with --apply to write these files.")
		}
		return
	}

	res, err := migrate.Apply(rep, migrate.ApplyOptions{Force: flags.Force})
	if flags.JSON {
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sMigration error:%s %v\n", cRed, cReset, err)
			os.Exit(1)
		}
		writeJSON(struct {
			Plan   *migrate.Report      `json:"plan"`
			Result *migrate.ApplyResult `json:"result"`
		}{Plan: rep, Result: res})
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sMigration error:%s %v\n", cRed, cReset, err)
		if len(rep.Manual) > 0 {
			printMigrationManual(rep)
		}
		os.Exit(1)
	}

	fmt.Printf("migrate: wrote %d file(s) from %s\n", len(res.Written), rep.Root)
	fmt.Printf("  backup: %s\n", res.BackupDir)
	fmt.Printf("  rollback: krate migrate rollback %s\n", res.BackupDir)
	if len(rep.Manual) > 0 {
		printMigrationManual(rep)
	}
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
		fmt.Printf("  %s %s -> %s\n", marker, src, f.Target)
		for _, r := range f.Rewrites {
			fmt.Printf("      %s\n", r)
		}
	}
	for _, w := range rep.Warnings {
		fmt.Printf("  warning: %s\n", w)
	}
	printMigrationManual(rep)
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
