// Package plugin is the Krate Native plugin system: reusable native
// capabilities written in Go, linked into the shell process of an app that
// enables them (native.plugins in krate.config.ts).
//
// A plugin is a Go package plus a krate-plugin.json manifest. The compiler
// reads the manifest, generates the call glue and the TypeScript module for the
// ops it lists exactly as it does for an app's own Go host code, and links
// the package into a per-app shell. Everything the plugin registers lives in
// its own namespace: the op foo of the plugin "sqlite" is the host id
// "plugin/sqlite.foo", gated by the permission "plugin:sqlite.<name>" its
// manifest declares for that op.
//
// # Writing a plugin
//
// Exported functions, streams (<-chan T) and signals (*krate.Signal[T]) that
// the manifest lists as ops (lowerCamel names, like the SDK ids; "info" binds to
// the Go function Info) are the plugin's API. A plugin that needs the
// platform, lifecycle hooks or hand-written ops also exports
//
//	func Init(c *plugin.Context) error
//
// which runs once while the shell loads, before the platform exists. Use
// c.OnStart for anything that needs the platform, c.Dispatch to run on the main
// thread, and c.Dialogs, c.Menus, c.WindowControls, c.Tray, c.Notifier for the
// optional platform capabilities.
//
// # Security
//
// A plugin is trusted Go code in the shell process (the user chooses to
// enable it, as with any dependency), so it can do whatever the process can.
// What the system enforces is the page-facing surface: a plugin cannot
// register an id outside plugin/<its name>. (Install fails), and every op
// call is checked against the app's permissions and the calling window's
// scope, like an SDK op.
package plugin
