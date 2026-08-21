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
		fmt.Fprintf(os.Stderr, "Usage: krate [flags] <build|dev|serve|version|init> [dir]\n")
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
	case "init":
		runInit(args)
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

func runInit(args []string) {
	dir := "."
	if len(args) > 1 {
		dir = args[1]
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}

	configPath := filepath.Join(dir, "krate.config.ts")
	if _, err := os.Stat(configPath); err == nil {
		fmt.Fprintf(os.Stderr, "%sAlready initialized:%s krate.config.ts exists in %s\n", cRed, dir, cReset)
		os.Exit(1)
	}

	depVersion := version
	if depVersion == "dev" {
		depVersion = "latest"
	}

	name := strings.ToLower(filepath.Base(dir))
	name = strings.NewReplacer(" ", "-", "_", "-").Replace(name)
	var clean strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			clean.WriteRune(r)
		}
	}
	name = clean.String()
	if name == "" {
		name = "krate-app"
	}

	files := []struct {
		path, content string
	}{
		{
			path: "krate.config.ts",
			content: `import { defineConfig } from '@krate/core';

export default defineConfig({
  entry: "src/pages/index.tsx",
  outDir: "dist",
  pagesDir: "src/pages",
  publicDir: "public",
  minify: true,
  devServer: {
    port: 3000,
    open: false,
  },
});
`,
		},
		{
			path: "package.json",
			content: fmt.Sprintf(`{
  "name": %q,
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "krate dev",
    "build": "krate build",
    "serve": "krate serve"
  },
  "dependencies": {
    "@krate/core": %q,
    "@krate/runtime": %q
  }
}
`, name, depVersion, depVersion),
		},
		{
			path: "tsconfig.json",
			content: `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "jsx": "preserve",
    "jsxImportSource": "@krate/runtime",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "resolveJsonModule": true
  },
  "include": ["src"]
}
`,
		},
		{
			path:    ".gitignore",
			content: "node_modules/\ndist/\n.krate/\n",
		},
		{
			path: "src/pages/index.tsx",
			content: `export default function Home() {
  return (
    <main>
      <h1>Welcome to Krate!</h1>
      <p>Your site is up and running.</p>
      <p>
        Edit <code>src/pages/index.tsx</code> and save to see hot reload.
      </p>
    </main>
  );
}
`,
		},
		{
			path: "src/pages/_layout.tsx",
			content: `export default function Layout({ children }) {
  return (
    <div>
      <header>
        <a href="/">Krate</a>
      </header>
      {children}
    </div>
  );
}
`,
		},
		{
			path:    "public/robots.txt",
			content: "User-agent: *\nAllow: /\n",
		},
		{
			path: "public/favicon.svg",
			content: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">
  <rect width="32" height="32" rx="6" fill="#00ADD8"/>
  <text x="16" y="23" font-family="sans-serif" font-size="18" font-weight="bold" text-anchor="middle" fill="#fff">K</text>
</svg>
`,
		},
	}

	var created, skipped []string
	for _, f := range files {
		abs := filepath.Join(dir, filepath.FromSlash(f.path))
		if _, err := os.Stat(abs); err == nil {
			skipped = append(skipped, f.path)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "%sError creating directory:%s %v\n", cRed, cReset, err)
			os.Exit(1)
		}
		if err := os.WriteFile(abs, []byte(f.content), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "%sError writing %s:%s %v\n", cRed, f.path, cReset, err)
			os.Exit(1)
		}
		created = append(created, f.path)
	}

	fmt.Printf("%s%s  Initialized Krate project in %s%s\n", cBold, cGreen, dir, cReset)
	for _, f := range created {
		fmt.Printf("%s  + %s%s\n", cGreen, f, cReset)
	}
	for _, f := range skipped {
		fmt.Printf("%s  ~ %s (exists, skipped)%s\n", cGray, f, cReset)
	}
	fmt.Printf("%s\nNext steps:%s\n", cBold, cReset)
	fmt.Printf("  cd %s\n", dir)
	fmt.Println("  npm install")
	fmt.Println("  npm run dev   # http://localhost:3000")
}
