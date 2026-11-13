package build

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/inlineaction"
)

// Inline server actions (a function with a leading "use server" directive
// inside a component) are hoisted into generated server-action modules under
// .krate/actions/src. At the original site the function becomes
// __krateInlineAction(id, [captured values], [names]).
//
// Closed-over values never travel to the browser in the clear: the server
// renderer seals them into a token with AES-256-GCM (key derived from
// KRATE_ACTION_SECRET, or from a key kept in .krate/action-key and copied to
// the action manifest), authenticated against the action id. The token rides in
// the action URL query as the first leading argument, and the action sidecar
// opens it and calls the hoisted function with the values first. A tampered or
// foreign token is rejected.

const (
	actionSecretEnv    = "KRATE_ACTION_SECRET"
	inlineHelperModule = "krate:inline-action"
	minActionSecretLen = 16
)

// hoistInlineActions hoists the inline actions of one project source file. It
// returns nil when the file has none.
func hoistInlineActions(root, abs, src string) (*inlineaction.Result, error) {
	if !inlineaction.Has(src) {
		return nil, nil
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		rel = abs
	}
	rel = filepath.ToSlash(rel)
	return inlineaction.Hoist(src, inlineaction.Options{
		Path:   rel,
		Rel:    rel,
		Dir:    filepath.Dir(abs),
		Client: bundler.HasUseClientDirective(src),
		Call:   inlineaction.Call,
	})
}

// hoistInlineModules writes one generated action module per source file that
// has inline actions, under dir/src, and returns them for compilation.
func (b *Builder) hoistInlineModules(dir string, files []string) ([]*actionModule, error) {
	var mods []*actionModule
	for _, abs := range files {
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		res, err := hoistInlineActions(b.Root, abs, string(data))
		if err != nil {
			return nil, err
		}
		if res == nil {
			continue
		}
		rel, _ := filepath.Rel(b.Root, abs)
		rel = filepath.ToSlash(rel)
		ext := ".tsx"
		if e := strings.ToLower(filepath.Ext(abs)); e == ".ts" || e == ".mts" {
			ext = ".ts"
		}
		srcDir := filepath.Join(dir, "src")
		if err := os.MkdirAll(srcDir, 0755); err != nil {
			return nil, fmt.Errorf("creating inline action dir: %w", err)
		}
		gen := filepath.Join(srcDir, hashContent([]byte(rel))+ext)
		if err := os.WriteFile(gen, []byte(res.Module), 0644); err != nil {
			return nil, fmt.Errorf("writing inline actions of %s: %w", rel, err)
		}
		mod := &actionModule{AbsPath: gen, RelPath: rel, Exports: map[string]string{}, Inline: map[string][]string{}}
		for _, a := range res.Actions {
			mod.Exports[a.Export] = a.ID
			mod.Inline[a.Export] = append([]string{}, a.Captures...)
		}
		mods = append(mods, mod)
	}
	return mods, nil
}

// readServerSource reads a project module for a server bundle with its inline
// actions hoisted. The bundle plugins that claim project modules read through
// it, so the hoisting does not depend on which of them loads a file.
func readServerSource(root, path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil || !inlineaction.Has(string(data)) || strings.Contains(filepath.ToSlash(path), "/node_modules/") {
		return data, err
	}
	res, err := hoistInlineActions(root, path, string(data))
	if err != nil {
		return nil, err
	}
	if res == nil {
		return data, nil
	}
	return []byte(withInlineActionImport(res.Source)), nil
}

// withInlineActionImport adds the helper import to rewritten source without
// changing its line numbers.
func withInlineActionImport(src string) string {
	return `import { __krateInlineAction } from "` + inlineHelperModule + `"; ` + src
}

// actionSecret returns the secret inline-action tokens are sealed with, and
// whether it comes from the environment (then it is not written anywhere).
func (b *Builder) actionSecret() (secret string, fromEnv bool, err error) {
	if v := strings.TrimSpace(os.Getenv(actionSecretEnv)); v != "" {
		if len(v) < minActionSecretLen {
			return "", true, fmt.Errorf("%s must be at least %d characters", actionSecretEnv, minActionSecretLen)
		}
		return v, true, nil
	}
	path := filepath.Join(b.Root, ".krate", "action-key")
	if data, rerr := os.ReadFile(path); rerr == nil {
		if v := strings.TrimSpace(string(data)); len(v) >= minActionSecretLen {
			return v, false, nil
		}
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", false, fmt.Errorf("creating the inline action key: %w", err)
	}
	secret = hex.EncodeToString(buf)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", false, err
	}
	if err := os.WriteFile(path, []byte(secret+"\n"), 0600); err != nil {
		return "", false, fmt.Errorf("writing the inline action key: %w", err)
	}
	return secret, false, nil
}

// inlineActionPlugin hoists inline actions out of project sources bundled for
// the server and serves the __krateInlineAction helper they call.
func inlineActionPlugin(root, basePath string) api.Plugin {
	return api.Plugin{
		Name: "krate-inline-actions",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^krate:inline-action$`}, func(api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{Path: inlineHelperModule, Namespace: "krate-inline-action"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-inline-action"}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
				src := inlineActionHelperSource(basePath)
				return api.OnLoadResult{Contents: &src, Loader: api.LoaderJS}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `\.(tsx?|jsx?|mjs|mts)$`}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				data, err := os.ReadFile(args.Path)
				if err != nil || !inlineaction.Has(string(data)) {
					return api.OnLoadResult{}, nil
				}
				out, err := readServerSource(root, args.Path)
				if err != nil {
					return api.OnLoadResult{}, err
				}
				if string(out) == string(data) {
					return api.OnLoadResult{}, nil
				}
				src := string(out)
				return api.OnLoadResult{Contents: &src, Loader: loaderForPath(args.Path), ResolveDir: filepath.Dir(args.Path)}, nil
			})
		},
	}
}

// inlineActionHelperSource is the server-side module behind
// __krateInlineAction. It builds the same proxy shape as the file-level action
// proxy (actionUrl for form rendering) and seals captured values.
func inlineActionHelperSource(basePath string) string {
	baseJSON, _ := json.Marshal(basePath)
	return "const BASE = " + string(baseJSON) + ";\n" + inlineActionHelperJS
}

const inlineActionHelperJS = `
import { createCipheriv, createHash, randomBytes } from "node:crypto";
import fs from "node:fs";
import path from "node:path";

const MAX_URL_TOKEN = 3500;
let sealKey = null;

function actionSecret() {
  let secret = process.env.KRATE_ACTION_SECRET || "";
  if (!secret && !globalThis[Symbol.for("krate.action.deployed")]) {
    const dir = process.env.KRATE_ACTION_DIR || path.join(process.env.KRATE_ROOT || process.cwd(), ".krate", "actions");
    try { secret = JSON.parse(fs.readFileSync(path.join(dir, "manifest.json"), "utf8")).key || ""; } catch {}
  }
  if (!secret) {
    throw new Error("inline server actions need an encryption key: set KRATE_ACTION_SECRET, or run the server from a build whose .krate/actions/manifest.json holds one");
  }
  return secret;
}

function key() {
  if (!sealKey) sealKey = createHash("sha256").update("krate-inline-action\0" + actionSecret()).digest();
  return sealKey;
}

function describe(v) {
  if (typeof v === "function") return "a function";
  if (typeof v === "symbol") return "a symbol";
  if (typeof v === "number") return "the number " + v;
  const proto = v && Object.getPrototypeOf(v);
  return (proto && proto.constructor && proto.constructor.name) ? "a " + proto.constructor.name + " instance" : "a non-plain object";
}

function encode(v, where, seen) {
  if (v === null || typeof v === "string" || typeof v === "boolean") return v;
  if (typeof v === "number") {
    if (!Number.isFinite(v)) throw new TypeError(where + ", which is " + describe(v));
    return v;
  }
  if (v === undefined) return { $: "u" };
  if (typeof v === "bigint") return { $: "n", v: v.toString() };
  if (v instanceof Date) return { $: "d", v: v.toISOString() };
  if (typeof v === "function" || typeof v === "symbol") throw new TypeError(where + ", which is " + describe(v));
  if (seen.has(v)) throw new TypeError(where + ", which is circular");
  seen.add(v);
  try {
    if (Array.isArray(v)) return v.map((x, i) => encode(x, where + "[" + i + "]", seen));
    const proto = Object.getPrototypeOf(v);
    if (proto !== Object.prototype && proto !== null) throw new TypeError(where + ", which is " + describe(v));
    const out = {};
    for (const k of Object.keys(v)) out[k] = encode(v[k], where + "." + k, seen);
    return Object.prototype.hasOwnProperty.call(out, "$") ? { $: "o", v: out } : out;
  } finally {
    seen.delete(v);
  }
}

function seal(id, values, names, label) {
  const encoded = values.map((v, i) => {
    try {
      return encode(v, '"' + names[i] + '"', new Set());
    } catch (e) {
      throw new TypeError(label + ' captures ' + e.message + ". Only strings, numbers, booleans, null, undefined, bigint, Date, and plain objects and arrays of those can be captured; read anything else inside the action.");
    }
  });
  const iv = randomBytes(12);
  const cipher = createCipheriv("aes-256-gcm", key(), iv);
  cipher.setAAD(Buffer.from(id));
  const body = Buffer.concat([cipher.update(JSON.stringify(encoded), "utf8"), cipher.final()]);
  const token = "k1." + Buffer.concat([iv, body, cipher.getAuthTag()]).toString("base64url");
  if (token.length > MAX_URL_TOKEN) {
    throw new RangeError(label + " closes over " + token.length + " bytes of data; capture ids and look the rest up inside the action");
  }
  return token;
}

export function __krateInlineAction(id, values, names, label) {
  let url = BASE + "/__krate/action/" + id;
  const bound = [];
  if (values.length > 0) {
    const token = seal(id, values, names, label);
    bound.push(token);
    url += "?__krate_args=" + encodeURIComponent(JSON.stringify([token]));
  }
  function action() {
    return Promise.reject(new Error("Server action cannot run during SSR"));
  }
  action.actionUrl = url;
  action.$$krateAction = id;
  action.$$krateBound = bound;
  action.bind = function () {
    throw new Error(label + ": .bind() is not supported on an inline server action; capture the value in the enclosing function instead");
  };
  action.toString = function () { return url; };
  return action;
}
`
