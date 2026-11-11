// Package sdk is the root of the Krate Native SDK: Go packages that app host
// code can call directly, plus registration functions that expose selected
// operations to the UI as host ids "krate/<area>.<name>".
//
// This file is the single source of truth for those ids. Each row names the
// permission that gates the id and the TypeScript signature of its wrapper in
// @krate/app. A Go test checks that the registrations match the table and
// that the TypeScript wrapper files mention every id.
//
// Area packages: sdk/os, sdk/fs, sdk/db, sdk/kv, sdk/net, sdk/permission.
// sdk/all registers everything.
package sdk

// ID describes one host id exposed to the UI.
type ID struct {
	// Name is the full host id, for example "krate/os.info".
	Name string
	// Permission is the app.json permission that gates it. Empty means always
	// allowed.
	Permission string
	// Args and Result are the TypeScript types of the wire arguments and the
	// result, for documentation and review.
	Args   string
	Result string
	// TS is the @krate/app module (file under packages/app/src) that wraps it.
	TS string
}

// IDs lists every SDK id. Other SDK areas append to the table in their own
// file of this package (see ids_*.go).
var IDs = []ID{
	{"krate/os.appDataDir", "os:info", "[]", "string", "os"},
	{"krate/os.cacheDir", "os:info", "[]", "string", "os"},
	{"krate/os.configDir", "os:info", "[]", "string", "os"},
	{"krate/os.logDir", "os:info", "[]", "string", "os"},
	{"krate/os.tempDir", "os:info", "[]", "string", "os"},
	{"krate/os.info", "os:info", "[]", "OsInfo", "os"},
	{"krate/os.openExternal", "shell:openExternal", "[url: string]", "void", "os"},
	{"krate/os.secretSet", "os:secrets", "[key: string, value: string]", "void", "os"},
	{"krate/os.secretGet", "os:secrets.read", "[key: string]", "string | null", "os"},
	{"krate/os.secretDelete", "os:secrets", "[key: string]", "void", "os"},

	{"krate/fs.readFile", "fs:read", "[path: string]", "Uint8Array", "fs"},
	{"krate/fs.readText", "fs:read", "[path: string]", "string", "fs"},
	{"krate/fs.readDir", "fs:read", "[path: string]", "DirEntry[]", "fs"},
	{"krate/fs.stat", "fs:read", "[path: string]", "FileInfo", "fs"},
	{"krate/fs.exists", "fs:read", "[path: string]", "boolean", "fs"},
	{"krate/fs.writeFile", "fs", "[path: string, data: Uint8Array]", "void", "fs"},
	{"krate/fs.writeText", "fs", "[path: string, text: string]", "void", "fs"},
	{"krate/fs.mkdir", "fs", "[path: string]", "void", "fs"},
	{"krate/fs.remove", "fs", "[path: string, recursive?: boolean]", "void", "fs"},
	{"krate/fs.rename", "fs", "[from: string, to: string]", "void", "fs"},

	{"krate/db.exec", "db", "[sql: string, params?: DbValue[]]", "ExecResult", "db"},
	{"krate/db.query", "db", "[sql: string, params?: DbValue[]]", "Row[]", "db"},
	{"krate/db.transaction", "db", "[statements: {sql: string, params?: DbValue[]}[]]", "ExecResult[]", "db"},

	{"krate/kv.get", "kv", "[key: string]", "unknown | null", "kv"},
	{"krate/kv.set", "kv", "[key: string, value: unknown]", "void", "kv"},
	{"krate/kv.delete", "kv", "[key: string]", "void", "kv"},
	{"krate/kv.keys", "kv", "[prefix?: string]", "string[]", "kv"},

	{"krate/permission.query", "", "[name: string]", "boolean", "permission"},
}

// Lookup returns the table row for id.
func Lookup(id string) (ID, bool) {
	for _, r := range IDs {
		if r.Name == id {
			return r, true
		}
	}
	return ID{}, false
}
