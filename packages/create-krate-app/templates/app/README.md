# __PROJECT_DISPLAY_NAME__

A [Krate](https://github.com/kratejs/krate) app scaffolded with
[`create-krate-app`](https://www.npmjs.com/package/create-krate-app).

Krate is a Go-native static site generator with signal-based reactivity. It
compiles TSX/JSX pages into static HTML at build time and ships a tiny
hydration bundle that makes pages interactive on the client.

## Getting started

Install dependencies and start the dev server:

```bash
npm install
npm run dev
```

Open [http://localhost:3000](http://localhost:3000) to see your app.

## Scripts

| Command          | Description                                     |
| ---------------- | ----------------------------------------------- |
| `npm run dev`    | Start the dev server with hot reload            |
| `npm run build`  | Build a production bundle into `dist/`          |
| `npm run serve`  | Build and serve the production output           |

## Project structure

```
.
├── krate.config.ts      # Krate configuration
├── tsconfig.json        # TypeScript configuration
├── public/              # Static assets served at /
└── src/
    ├── pages/           # File-based routes (pages/layouts/404)
    └── components/      # Reusable components
```

- `src/pages/index.tsx` maps to `/`
- `src/pages/_layout.tsx` wraps every page in a shared shell
- `src/pages/404.tsx` is the not-found page

## Learn more

- [Krate documentation](https://krate.js.org/docs/)
- [Getting started guide](https://krate.js.org/docs/getting-started/)
- [GitHub](https://github.com/kratejs/krate)
