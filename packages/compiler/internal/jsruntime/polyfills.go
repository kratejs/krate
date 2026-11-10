package jsruntime

import (
	"encoding/json"
	"fmt"
	"os"
)

// nodeGlobalsSource defines process (env only; SetEnv fills it) and a minimal
// require() for "fs" and "path".
var nodeGlobalsSource = `
		var process = {
			env: {}
		};

		function __require_fs() {
			return {
				readFileSync: function(p, opts) { return _go_readFile(p, typeof opts === 'object' ? opts : {encoding: 'utf-8'}); },
				readFile: function(p, opts, cb) {
					if (typeof opts === 'function') { cb = opts; opts = {encoding: 'utf-8'}; }
					try { var data = _go_readFile(p, typeof opts === 'object' ? opts : {encoding: 'utf-8'}); cb(null, data); }
					catch(e) { cb(e); }
				}
			};
		}
		function __require_path() {
			return {
				join: function() { var parts = []; for (var i = 0; i < arguments.length; i++) parts.push(arguments[i]); return parts.join('/').replace(/\\\\+/g, '/'); },
				resolve: function() { var parts = []; for (var i = 0; i < arguments.length; i++) parts.push(arguments[i]); return parts.join('/').replace(/\\\\+/g, '/'); },
				sep: '/',
				basename: function(p, ext) { var name = p.split('/').pop().split('\\\\').pop(); if (ext && name.endsWith(ext)) name = name.slice(0, -ext.length); return name; },
				dirname: function(p) { var parts = p.split('/'); parts.pop(); return parts.join('/') || '.'; },
				extname: function(p) { var idx = p.lastIndexOf('.'); return idx >= 0 ? p.slice(idx) : ''; }
			};
		}
		if (typeof require === 'undefined') {
			globalThis.require = function(m) {
				if (m === 'fs') return __require_fs();
				if (m === 'path') return __require_path();
				throw new Error('Module not found: ' + m);
			};
		}
	`

var nodeGlobalsBC sharedBytecode

// injectNodeGlobals installs process.env and the fs/path require() shim; the
// fs shim reads files through the _go_readFile host function.
func (r *Runtime) injectNodeGlobals() error {
	if err := r.registerHost("_go_readFile", func(args []any) (any, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("fs.readFile requires a path")
		}
		path, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("fs.readFile path must be a string")
		}
		encoding := "utf-8"
		if len(args) > 1 {
			if opts, ok := args[1].(map[string]any); ok {
				if enc, ok := opts["encoding"].(string); ok {
					encoding = enc
				}
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", path, err)
		}
		if encoding == "utf-8" || encoding == "utf8" || encoding == "" {
			return string(data), nil
		}
		return nil, fmt.Errorf("unsupported encoding: %s", encoding)
	}); err != nil {
		return fmt.Errorf("fs.readFile: %w", err)
	}
	bc, err := nodeGlobalsBC.get(nodeGlobalsSource)
	if err != nil {
		return err
	}
	_, err = r.EvalBytecode(bc)
	return err
}

// SetEnv sets environment variables for the runtime. Values are JSON-encoded
// so keys/values containing quotes or backslashes are safe.
func (r *Runtime) SetEnv(env map[string]string) error {
	if env == nil {
		env = map[string]string{}
	}
	b, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshaling env: %w", err)
	}
	_, err = r.Execute(fmt.Sprintf("process.env = %s;", string(b)))
	return err
}
