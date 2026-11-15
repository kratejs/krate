package migrate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const nextAuthFixtureConfig = `import { DrizzleAdapter } from '@auth/drizzle-adapter';
import NextAuth from 'next-auth';
import Discord from 'next-auth/providers/discord';
import { accounts, getDb, sessions, users, verificationTokens } from '@/db';

export const { handlers, auth, signIn, signOut } = NextAuth(() => ({
  adapter: DrizzleAdapter(getDb(), {
    usersTable: users,
    accountsTable: accounts,
    sessionsTable: sessions,
    verificationTokensTable: verificationTokens,
  }),
  session: { strategy: 'database' },
  providers: [
    Discord({
      clientId: process.env.AUTH_DISCORD_ID,
      issuer: 'https://discord.com',
      authorization: { params: { scope: 'identify guilds' } },
    }),
  ],
  pages: { signIn: '/signin' },
  trustHost: true,
  callbacks: {
    session({ session, user }) {
      return { expires: session.expires, user: { id: user.id } };
    },
  },
}));
`

func nextAuthProject(t *testing.T, config string) string {
	return gapsProject(t, map[string]string{
		"package.json":       `{"dependencies":{"next":"16.0.0","next-auth":"5.0.0-beta.32","@auth/drizzle-adapter":"^1.11.3"}}`,
		"tsconfig.json":      `{"compilerOptions":{"paths":{"@/*":["./src/*"]}}}`,
		"src/auth.ts":        config,
		"src/db/index.ts":    `export const users = {}, accounts = {}, sessions = {}, verificationTokens = {}; export function getDb() { return {}; }`,
		"src/app/layout.tsx": `export default function L({ children }) { return <html><body>{children}</body></html>; }`,
		"src/app/api/auth/[...nextauth]/route.ts": "import { handlers } from '@/auth';\n\nexport const { GET, POST } = handlers;\n",
		"src/app/signin/popup/route.ts":           "import { signIn } from '@/auth';\n\nexport async function GET() {\n  await signIn('discord', { redirectTo: '/signin/done' });\n}\n",
		"src/components/user-menu.tsx":            "import { signOut } from '@/auth';\n\nexport function UserMenu() {\n  async function out() {\n    'use server';\n    await signOut({ redirectTo: '/' });\n  }\n  return <form action={out}><button>Sign out</button></form>;\n}\n",
		"src/app/page.tsx":                        "import { auth } from '@/auth';\nimport { UserMenu } from '@/components/user-menu';\n\nexport default async function Home() {\n  const session = await auth();\n  return <main>{session?.user ? <UserMenu /> : <a href=\"/signin/popup\">Sign in</a>}</main>;\n}\n",
	})
}

func TestNextAuthIsPortedToKrateAuth(t *testing.T) {
	root := nextAuthProject(t, nextAuthFixtureConfig)
	rep, err := ScanNextWithOptions(root, ScanOptions{UpdatePackage: true})
	if err != nil {
		t.Fatal(err)
	}
	cfg := findPlan(rep, "src/auth.ts")
	if cfg == nil || cfg.Kind != KindModule {
		t.Fatalf("src/auth.ts was not planned as a module: %+v", rep.Files)
	}
	body := string(cfg.Content)
	for _, want := range []string{
		"import { defineAuth, discord as Discord } from '@krate/auth';",
		"export const { handlers, auth, signIn, signOut } = defineAuth(() => ({",
		"cookies: { prefix: 'authjs' },",
		"import { DrizzleAdapter } from '@auth/drizzle-adapter';",
		"session: { strategy: 'database' },",
		"authorization: { params: { scope: 'identify guilds' } },",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("migrated config lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "next-auth") {
		t.Errorf("migrated config still mentions next-auth:\n%s", body)
	}
	// The route, the server action and the server component keep their imports of
	// the project's own auth module.
	if route := findPlan(rep, "src/api/auth/[...nextauth].ts"); route == nil || !strings.Contains(string(route.Content), "from '@/auth'") {
		t.Errorf("auth route was not planned unchanged: %+v", route)
	}
	pkg := findPlan(rep, "package.json")
	if pkg == nil || !strings.Contains(string(pkg.Content), `"@krate/auth"`) {
		t.Errorf("package plan does not add @krate/auth: %+v", pkg)
	}
	for _, m := range rep.Manual {
		if m.Source != "package.json" {
			t.Errorf("a supported next-auth setup left a manual item: %+v", m)
		}
	}
}

func TestNextAuthUnportableShapesAreManualItems(t *testing.T) {
	root := nextAuthProject(t, `import NextAuth from "next-auth";
import GitHub from "next-auth/providers/github";
import Nodemailer from "next-auth/providers/nodemailer";
import { getToken } from "next-auth/jwt";

export const { handlers, auth, signIn, signOut } = NextAuth({
  providers: [GitHub, Nodemailer({ server: "smtp://x" })],
  theme: { logo: "/x.png" },
  cookies: { sessionToken: { name: "my-session" } },
  callbacks: { async authorized() { return true; }, async nope() { return true; } },
});
`)
	writeNextFile(t, root, "src/middleware.ts", "import { auth } from '@/auth';\n\nexport default auth((req) => {});\n")
	writeNextFile(t, root, "src/types/next-auth.d.ts", "import 'next-auth';\ndeclare module 'next-auth' { interface Session { x: string } }\n")
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ source, reason string }{
		{"src/auth.ts", "providers/nodemailer"},
		{"src/auth.ts", "option theme"},
		{"src/auth.ts", "cookies.sessionToken"},
		{"src/auth.ts", "callbacks.nope"},
		{"src/auth.ts", "next-auth/jwt"},
		{"src/middleware.ts", "auth(handler)"},
		{"src/types/next-auth.d.ts", "augment next-auth"},
		{"package.json", "next-auth is still listed"},
	} {
		if !hasManual(rep, want.source, want.reason) {
			t.Errorf("no manual item for %s about %q: %+v", want.source, want.reason, rep.Manual)
		}
	}
	// The sessionToken override keeps cookies.prefix out of the config.
	if cfg := findPlan(rep, "src/auth.ts"); cfg == nil || strings.Contains(string(cfg.Content), "prefix: \"authjs\"") || !strings.Contains(string(cfg.Content), "defineAuth({") {
		t.Errorf("config was not ported as expected: %+v", cfg)
	}
}

// Every provider the migrator maps must exist in the @krate/auth catalog, and
// every catalog provider must be mapped.
func TestNextAuthProviderTableMatchesCatalog(t *testing.T) {
	catalog, err := os.ReadFile(filepath.Join("..", "..", "..", "auth", "src", "providers", "catalog.ts"))
	if err != nil {
		t.Skipf("@krate/auth sources not available: %v", err)
	}
	block := regexp.MustCompile(`(?s)export const catalog[^{]*\{(.*?)\n\};`).FindSubmatch(catalog)
	if block == nil {
		t.Fatal("catalog record not found")
	}
	exported := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^export const (\w+) = `).FindAllSubmatch(catalog, -1) {
		exported[string(m[1])] = true
	}
	ids := regexp.MustCompile(`(?m)^\s*(?:"([^"]+)"|(\w+))(?::\s*(\w+))?,\s*$`).FindAllSubmatch(block[1], -1)
	if len(ids) < 50 {
		t.Fatalf("parsed %d catalog entries", len(ids))
	}
	for _, m := range ids {
		id, export := string(m[1]), string(m[3])
		if id == "" {
			id, export = string(m[2]), string(m[2])
		}
		if !exported[export] {
			t.Errorf("catalog entry %s: export %s not found", id, export)
		}
		slug := id
		if id == "duende-identityserver6" {
			slug = "duende-identity-server6"
		}
		got, ok := authProviderExport(slug)
		if !ok || got != export {
			t.Errorf("provider %s maps to %q (ok=%v), want %s", slug, got, ok, export)
		}
	}
}
