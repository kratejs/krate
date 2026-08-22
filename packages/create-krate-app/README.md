# create-krate-app

Scaffold a new [Krate](https://github.com/kratejs/krate) app in seconds.

```bash
npm create krate-app@latest my-app
# or
npx create-krate-app@latest my-app
```

You can also use `pnpm`, `yarn`, or `bun`:

```bash
pnpm create krate-app my-app
bun create krate-app my-app
```

## Usage

```
create-krate-app [project-directory] [options]
```

| Option          | Description                              |
| --------------- | ---------------------------------------- |
| `--use-npm`     | Install dependencies with npm            |
| `--use-pnpm`    | Install dependencies with pnpm           |
| `--use-yarn`    | Install dependencies with yarn           |
| `--use-bun`     | Install dependencies with bun            |
| `--skip-install`| Skip dependency installation             |
| `--no-git`      | Do not initialize a git repository       |
| `-h, --help`    | Show help                                |
| `-v, --version` | Show the CLI version                     |

Run without a project directory to be prompted for a name.

## What you get

A ready-to-run Krate project:

- `krate.config.ts` — Krate configuration
- `tsconfig.json` — TypeScript config with `@/*` path aliases
- `package.json` — `dev`/`build`/`serve` scripts
- `src/pages/` — file-based routes with a layout and 404 page
- `src/components/` — a signal-powered `Counter` example (with CSS Modules)
- `public/` — static assets and global styles

## License

Apache-2.0
