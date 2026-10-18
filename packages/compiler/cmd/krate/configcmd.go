package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// runConfig prints the fully resolved configuration (after defaults, `site`
// folding, path resolution, and CLI overrides) as JSON. With `--schema` it
// prints a JSON Schema for krate.config.* instead.
func runConfig(flags cliFlags, args []string) {
	if flags.Schema {
		data, err := config.SchemaJSON()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
			os.Exit(1)
		}
		fmt.Println(string(data))
		return
	}
	_, cfg := resolveConfig(flags, args)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}
