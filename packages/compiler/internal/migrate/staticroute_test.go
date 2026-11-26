package migrate

import (
	"strings"
	"testing"
)

func TestMigrateStaticExportRouteHandlers(t *testing.T) {
	rep := scanConfig(t, map[string]string{
		"next.config.ts": `export default { output: 'export' };`,
		"app/page.tsx":   `export default function Home() { return <main>Home</main>; }`,
		"app/feed.txt/route.ts": `export const dynamic = "force-static";
export async function GET() {
  return new Response("hello\n", { headers: { "Content-Type": "text/plain" } });
}
`,
		"app/notes/[[...slug]]/route.ts": `export function generateStaticParams() {
  return [{ slug: [] }, { slug: ["intro"] }];
}
export async function GET(_req: Request, { params }: { params: Promise<{ slug?: string[] }> }) {
  const { slug } = await params;
  return new Response((slug || []).join("/"));
}
`,
		"app/cards/[...slug]/route.tsx": `import { ImageResponse } from "next/og";
export function generateStaticParams() {
  return [{ slug: ["intro"] }];
}
export async function GET() {
  return new ImageResponse(<div>Card</div>, { width: 600, height: 300 });
}
`,
		"app/api/catalog/route.ts": `export async function GET() {
  return Response.json({ ok: true });
}
`,
		"app/api/submit/route.ts": `export async function POST(req: Request) {
  return Response.json({ ok: true });
}
`,
	})

	body := configBody(t, rep)
	if !strings.Contains(body, `"output": "static"`) {
		t.Errorf("krate.config.ts lacks output: \"static\":\n%s", body)
	}
	if strings.Contains(body, `"rewrites"`) {
		t.Errorf("static GET route handlers should not add /api rewrites:\n%s", body)
	}
	if hasManual(rep, "next.config.ts", "output") {
		t.Errorf("output: 'export' should be translated without a manual item: %+v", rep.Manual)
	}
	if !hasInfo(rep, "output: 'export' became output: \"static\"") {
		t.Errorf("missing info note for output: 'export': %+v", rep.Info)
	}

	// Non-api static GET routes move under src/pages/**/route.* at their original URL.
	for _, want := range []struct {
		target string
		route  string
		substr string
	}{
		{"src/pages/feed.txt/route.ts", "/feed.txt", "force-static"},
		{"src/pages/notes/[[...slug]]/route.ts", "/notes/[[...slug]]", "generateStaticParams"},
		{"src/pages/cards/[...slug]/route.tsx", "/cards/[...slug]", "@krate/runtime/og"},
		{"src/api/catalog.ts", "/api/catalog", "GET"},
	} {
		pf := findPlan(rep, want.target)
		if pf == nil {
			t.Errorf("missing planned file %s", want.target)
			continue
		}
		if pf.Kind != KindStaticRoute {
			t.Errorf("%s kind = %s, want %s", want.target, pf.Kind, KindStaticRoute)
		}
		if !strings.Contains(string(pf.Content), want.substr) {
			t.Errorf("%s content lacks %q:\n%s", want.target, want.substr, string(pf.Content))
		}
		foundRoute := false
		for _, r := range rep.Routes {
			if r.Target == want.target && r.Route == want.route {
				foundRoute = true
			}
		}
		if !foundRoute {
			t.Errorf("route inventory missing (%s -> %s): %+v", want.target, want.route, rep.Routes)
		}
	}

	// POST route under output: 'export' stays KindAPI and gets a manual item.
	submit := findPlan(rep, "src/api/submit.ts")
	if submit == nil || submit.Kind != KindAPI {
		t.Errorf("POST route should stay KindAPI: %+v", submit)
	}
	if !hasManual(rep, "app/api/submit/route.ts", "non-GET") {
		t.Errorf("expected manual item for POST route under output: 'export': %+v", rep.Manual)
	}
}

func TestMigrateForceStaticWithoutExportAndRequestTimeFallback(t *testing.T) {
	rep := scanConfig(t, map[string]string{
		"next.config.ts": `export default {};`,
		"app/page.tsx":   `export default function Home() { return <main>Home</main>; }`,
		"app/summary.txt/route.ts": `export const dynamic = "force-static";
export function GET() { return new Response("summary"); }
`,
		"app/go/[code]/route.ts": `export function GET() { return new Response("dynamic"); }
`,
	})

	// force-static handler migrates to src/pages/summary.txt/route.ts with no /api rewrite.
	summary := findPlan(rep, "src/pages/summary.txt/route.ts")
	if summary == nil || summary.Kind != KindStaticRoute {
		t.Fatalf("force-static handler should become src/pages/summary.txt/route.ts (KindStaticRoute): %+v", summary)
	}

	// Request-time handler keeps old behavior: src/api/go/[code].ts + /api rewrite.
	dyn := findPlan(rep, "src/api/go/[code].ts")
	if dyn == nil || dyn.Kind != KindAPI {
		t.Fatalf("request-time handler should stay src/api/go/[code].ts (KindAPI): %+v", dyn)
	}
	body := configBody(t, rep)
	if !strings.Contains(body, `"/go/:code"`) || !strings.Contains(body, `"/api/go/:code"`) {
		t.Errorf("request-time handler rewrite missing:\n%s", body)
	}
	if strings.Contains(body, "summary.txt") {
		t.Errorf("force-static handler should not get an /api rewrite:\n%s", body)
	}
}
