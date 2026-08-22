package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"krate-compiler/internal/build"
	"krate-compiler/internal/config"
)

const (
	cReset = "\033[0m"
	cRed   = "\033[31m"
	cGreen = "\033[32m"
	cCyan  = "\033[36m"
	cGray  = "\033[90m"
	cBold  = "\033[1m"
)

// version is set at build time via `-ldflags "-X main.version=<version>"`.
// Local/dev builds default to "dev".
var version = "dev"

type cliFlags struct {
	ConfigPath string
	OutDir     string
	Watch      bool
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	flags, args := parseFlags(os.Args[1:])

	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: krate [flags] <build|dev|serve|version> [dir]\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fmt.Fprintf(os.Stderr, "  --config <path>   Path to config file (default: project/krate.config.ts)\n")
		fmt.Fprintf(os.Stderr, "  --out-dir <path>  Override output directory\n")
		fmt.Fprintf(os.Stderr, "  --watch           Rebuild on file changes\n")
		os.Exit(1)
	}

	switch args[0] {
	case "build":
		runBuild(flags, args)
	case "dev":
		runDev(flags, args)
	case "serve":
		runServe(flags, args)
	case "version":
		fmt.Println("krate v" + version)
	default:
		fmt.Fprintf(os.Stderr, "%sUnknown command:%s %s\n", cRed, cReset, args[0])
		os.Exit(1)
	}
}

func parseFlags(args []string) (cliFlags, []string) {
	var flags cliFlags
	var remaining []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--config" && i+1 < len(args):
			flags.ConfigPath = args[i+1]
			i++
		case args[i] == "--out-dir" && i+1 < len(args):
			flags.OutDir = args[i+1]
			i++
		case args[i] == "--watch":
			flags.Watch = true
		case strings.HasPrefix(args[i], "-"):
			fmt.Fprintf(os.Stderr, "%sUnknown flag:%s %s\n", cRed, cReset, args[i])
			os.Exit(1)
		default:
			remaining = append(remaining, args[i])
		}
	}
	return flags, remaining
}

func resolveConfig(flags cliFlags, args []string) (string, *config.Config) {
	root := "."
	if len(args) > 1 {
		root = args[1]
	}
	root, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}

	cfg, err := config.Load(root, flags.ConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sConfig error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}
	cfg.Resolve(root)

	if flags.OutDir != "" {
		if filepath.IsAbs(flags.OutDir) {
			cfg.OutDir = flags.OutDir
		} else {
			cfg.OutDir = filepath.Join(root, flags.OutDir)
		}
	}

	return root, cfg
}

func runBuild(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)

	fmt.Printf("%s%s  Building %s \u2192 %s%s\n", cBold, cCyan, root, cfg.OutDir, cReset)

	start := time.Now()
	builder := build.New(root, cfg)
	if err := builder.BuildAll(); err != nil {
		fmt.Fprintf(os.Stderr, "%sBuild error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}

	fmt.Printf("%s%s  Done! (built in %s)%s\n", cBold, cGreen, time.Since(start).Round(time.Millisecond), cReset)

	if flags.Watch {
		reload := make(chan []string, 1)
		errc := make(chan error, 1)
		go func() {
			if err := build.Watch(root, cfg, 500*time.Millisecond, reload); err != nil {
				errc <- err
			}
		}()
		go func() {
			for routes := range reload {
				fmt.Printf("\n%s%s  Rebuilt:%s %v (%s)\n", cBold, cCyan, cReset, routes, time.Now().Format("15:04:05"))
			}
		}()
		err := <-errc
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}
}

func runDev(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)

	fmt.Printf("%s%s  Starting dev server...%s\n", cBold, cCyan, cReset)
	start := time.Now()

	builder := build.New(root, cfg)
	builder.DevMode = true
	if err := builder.BuildAll(); err != nil {
		fmt.Fprintf(os.Stderr, "%sBuild error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}

	reload := make(chan []string, 1)
	errc := make(chan error, 2)

	go func() {
		if err := build.ServeDev(root, cfg, reload, start); err != nil {
			errc <- err
		}
	}()

	go func() {
		if err := build.Watch(root, cfg, 500*time.Millisecond, reload); err != nil {
			errc <- err
		}
	}()

	err := <-errc
	fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
	os.Exit(1)
}

func runServe(flags cliFlags, args []string) {
	root, cfg := resolveConfig(flags, args)

	fmt.Printf("%s%s  Building for preview...%s\n", cBold, cCyan, cReset)
	start := time.Now()

	builder := build.New(root, cfg)
	if err := builder.BuildAll(); err != nil {
		fmt.Fprintf(os.Stderr, "%sBuild error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}

	fmt.Printf("%s%s  Build complete. Starting server...%s\n", cBold, cGreen, cReset)

	if err := build.Serve(root, cfg, start); err != nil {
		fmt.Fprintf(os.Stderr, "%sServe error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}
}

