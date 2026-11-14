package migrate

import (
	"strings"
	"testing"
)

func TestSegmentConfigIsDroppedAndAggregated(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/api/a/route.ts": `import { NextResponse } from "next/server";
export const runtime = 'nodejs';
export const maxDuration = 60;
export async function GET() { return NextResponse.json({}); }`,
		"app/api/b/route.ts": `export const runtime = "edge";
export const maxDuration = 300;
export const preferredRegion = ['iad1', 'fra1'];
export const fetchCache = 'force-no-store';
export async function GET() { return Response.json({}); }`,
		"app/api/c/route.ts": `export const fetchCache = 'default-no-store';
export async function GET() { return Response.json({}); }`,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	a := string(findPlan(rep, "src/api/a.ts").Content)
	if strings.Contains(a, "runtime") {
		t.Errorf("runtime not dropped:\n%s", a)
	}
	if !strings.Contains(a, "export const maxDuration = 60;") {
		t.Errorf("API route maxDuration must be kept:\n%s", a)
	}
	b := string(findPlan(rep, "src/api/b.ts").Content)
	if strings.Contains(b, "preferredRegion") || strings.Contains(b, "runtime") {
		t.Errorf("segment config not dropped:\n%s", b)
	}
	if !strings.Contains(b, "export const maxDuration = 300;") {
		t.Errorf("API route maxDuration must be kept:\n%s", b)
	}
	if !strings.Contains(b, "fetchCache") {
		t.Errorf("fetchCache must stay:\n%s", b)
	}
	var segment []string
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "segment config") {
			segment = append(segment, m.Reason)
		}
	}
	if len(segment) != 1 || !strings.Contains(segment[0], "2 files") {
		t.Errorf("want one aggregated fetchCache manual item, got %q", segment)
	}
	joined := strings.Join(rep.Warnings, "\n")
	for _, want := range []string{"export const runtime from 2 files", "preferredRegion from 1 files"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing aggregated warning %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "maxDuration") {
		t.Errorf("a kept maxDuration must not warn:\n%s", joined)
	}
}

func TestSegmentConfigKeptWhenReferenced(t *testing.T) {
	src, notes := dropSegmentConfig("export const runtime = 'nodejs';\nexport function GET() { return Response.json({ runtime }); }\n", KindAPI)
	if len(notes) != 0 || !strings.Contains(src, "export const runtime") {
		t.Errorf("a referenced export must stay: %q %v", src, notes)
	}
}

func TestMaxDurationKeptInAPIRoutesOnly(t *testing.T) {
	const decl = "export const maxDuration = 120;\n"
	src, notes := dropSegmentConfig(decl, KindAPI)
	if src != decl || len(notes) != 1 || !strings.Contains(notes[0], "request timeout") {
		t.Errorf("API route: want kept with a note, got %q %v", src, notes)
	}
	src, notes = dropSegmentConfig(decl, KindPage)
	if src != "" || len(notes) != 1 || !strings.Contains(notes[0], "only API routes") {
		t.Errorf("page: want dropped, got %q %v", src, notes)
	}
}

func TestNextRequestCookiesNextURLAndAfter(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/api/cb/route.ts": `import { NextRequest, NextResponse, after } from "next/server";
export async function GET(request: NextRequest) {
  const code = request.nextUrl.searchParams.get("code");
  const state = request.cookies.get("state")?.value;
  after(async () => { console.log("done", code, state); });
  const res = NextResponse.redirect(new URL("/", request.nextUrl));
  res.cookies.set("session", "abc", { httpOnly: true, maxAge: 60 });
  return res;
}`,
		"app/api/w/route.ts": `import { NextRequest, NextResponse } from "next/server";
export async function POST(request: NextRequest) {
  request.cookies.set("x", "1");
  return NextResponse.json({});
}`,
		"app/api/u/route.ts": `import { NextResponse, userAgent } from "next/server";
export async function GET() { return NextResponse.json({}); }`,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	cb := findPlan(rep, "src/api/cb.ts")
	body := string(cb.Content)
	if !strings.Contains(body, "import { after } from \"@krate/runtime/server\";") || !strings.Contains(body, "import type { KrateRequest }") {
		t.Errorf("after() not mapped to the runtime:\n%s", body)
	}
	if strings.Contains(body, "next/server") {
		t.Errorf("next/server import left:\n%s", body)
	}
	if !strings.Contains(body, "__krateWithCookies") || !strings.Contains(body, "set-cookie") {
		t.Errorf("response cookies shim missing:\n%s", body)
	}
	for _, m := range rep.Manual {
		if m.Source == "app/api/cb/route.ts" {
			t.Errorf("cb handler should need no manual work: %+v", m)
		}
	}
	if !hasManualReason(rep, "app/api/w/route.ts", "request.cookies is read-only") {
		t.Errorf("request.cookies.set must be flagged: %+v", rep.Manual)
	}
	if !hasManualReason(rep, "app/api/u/route.ts", "unsupported helper userAgent") {
		t.Errorf("userAgent must be flagged: %+v", rep.Manual)
	}
	if hasManualReason(rep, "app/api/u/route.ts", "next/server request/response helpers need") {
		t.Errorf("generic next/server item must be gone for route handlers")
	}
}

func hasManualReason(rep *Report, source, contains string) bool {
	for _, m := range rep.Manual {
		if m.Source == source && strings.Contains(m.Reason, contains) {
			return true
		}
	}
	return false
}
