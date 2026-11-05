# @krate/ts-plugin

TypeScript language service plugin for [Krate](https://krate.js.org). It stops
editors reporting tier decorators such as `@server` as errors.

```json
{
  "compilerOptions": {
    "plugins": [{ "name": "@krate/ts-plugin" }]
  }
}
```

Language service plugins only run in editors. Use `krate typecheck` instead of
`tsc --noEmit` on the command line. In VS Code, select the workspace TypeScript
version so the plugin loads.
