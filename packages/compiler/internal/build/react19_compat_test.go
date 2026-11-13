package build

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

const counterBody = `import { useState } from "react";
export default function Counter() {
  const [n, setN] = useState(0);
  return <button onClick={() => setN(n + 1)}>{n}</button>;
}`

const decoratedCounter = `import { useState } from "react";
@static
export default function Counter() {
  const [n, setN] = useState(0);
  return <button onClick={() => setN(n + 1)}>{n}</button>;
}`

// 1. Client APIs in build-time tiers fail the build.

func TestBuildTimeTierRejectsClientAPIs(t *testing.T) {
	cases := map[string]struct {
		files   map[string]string
		contain string
	}{
		"server comment": {map[string]string{
			"src/pages/index.tsx": "// @server\n" + counterBody,
		}, "useState() in Counter"},
		"server file name": {map[string]string{
			"src/pages/index.tsx": `// @server
import Inner from "../Inner.server";
export default function P() { return <main><Inner /></main>; }`,
			"src/Inner.server.tsx": counterBody,
		}, "useState() in Counter"},
		"static decorator": {map[string]string{
			"src/pages/index.tsx": `import Inner from "../Inner";
export default function P() { return <main><Inner /></main>; }`,
			"src/Inner.tsx": decoratedCounter,
		}, "a static component"},
		"server decorator on a helper": {map[string]string{
			"src/pages/index.tsx": `import { useState } from "react";
@server
function Live() { const [n, setN] = useState(0); return <button onClick={() => setN(n + 1)}>{n}</button>; }
export default function P() { return <main><Live /></main>; }`,
		}, "a server component"},
		"effect": {map[string]string{
			"src/pages/index.tsx": `// @server
import { useEffect } from "react";
export default function P() { useEffect(() => { document.title = "x"; }, []); return <main>hi</main>; }`,
		}, "useEffect() in P"},
		"handler only": {map[string]string{
			"src/pages/index.tsx": `// @server
export default function P() { return <button onClick={() => alert(1)}>go</button>; }`,
		}, "event handler onClick on <button>"},
		"class component": {map[string]string{
			"src/pages/index.tsx": `// @server
import { Component } from "react";
export default class P extends Component {
  state = { n: 0 };
  render() { return <button onClick={() => this.setState({ n: this.state.n + 1 })}>{this.state.n}</button>; }
}`,
		}, "a server component"},
		"class lifecycle": {map[string]string{
			"src/pages/index.tsx": `// @server
import { Component } from "react";
export default class P extends Component {
  componentDidMount() { document.title = "x"; }
  render() { return <p>hi</p>; }
}`,
		}, "componentDidMount()"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := buildErr(t, tc.files)
			wantCode(t, err, diag.CodeClientAPIInServer, tc.contain)
			if !strings.Contains(err.Error(), "client child component") {
				t.Errorf("the hint should explain the fix: %v", err)
			}
		})
	}
}
