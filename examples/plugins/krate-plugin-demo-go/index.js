// krate-plugin-demo-go - a community plugin written in Go.
// This file is the plugin's npm-style descriptor. When Krate loads the plugin it
// bundles this module and runs the factory once to discover the plugin's
// runtime and per-platform binaries (the manifest is static - no Node builtins).
//   - runtime:   'go' routes this plugin through the Go subprocess host.
//   - binaries:  a map from GOOS-GOARCH to the plugin binary shipped in this
//                package (relative to the package root).
// The plugin author builds one binary per platform and places them under bin/.
// Krate picks the binary matching the host, spawns it via HashiCorp go-plugin
// (net/rpc), and keeps it alive across dev hot-reloads.
// To wire into krate.config.ts:
//	import demoGoPlugin from 'krate-plugin-demo-go';
//	export default { plugins: [demoGoPlugin()] };
module.exports = function() {
  return {
    name: "demo-go",
    order: 10,
    // Points Krate at this manifest module itself so relative binary paths can
    // be resolved against the package root. In ESM contexts import.meta.url is
    // a file:// URL; the compiler converts it to a filesystem path.
    module: (typeof import.meta !== "undefined" && import.meta.url) ? import.meta.url : "",
    runtime: "go",
    hooks: { BeforeBuild: null, AfterParse: null, AfterRender: null, ServeRequest: null, ServeResponse: null },
    binaries: {
      "windows-amd64": "bin/krate-plugin-demo-go-windows-amd64.exe",
      "darwin-amd64": "bin/krate-plugin-demo-go-darwin-amd64",
      "darwin-arm64": "bin/krate-plugin-demo-go-darwin-arm64",
      "linux-amd64": "bin/krate-plugin-demo-go-linux-amd64",
      "linux-arm64": "bin/krate-plugin-demo-go-linux-arm64",
    },
  };
};
