package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/migrate"
)

const nextAuthMigrateConfig = `import { DrizzleAdapter } from '@auth/drizzle-adapter';
import NextAuth from 'next-auth';
import Discord from 'next-auth/providers/discord';
import { accounts, getDb, sessions, users, verificationTokens } from './db';

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
      clientSecret: process.env.AUTH_DISCORD_SECRET,
      issuer: 'https://discord.com',
      authorization: { params: { scope: 'identify guilds' } },
    }),
  ],
  pages: { signIn: '/signin' },
  trustHost: true,
}));
`

// Relative imports keep this test off the server bundles' tsconfig paths handling.
// A Next app using next-auth migrates to @krate/auth, builds with no next-auth
// installed, and serves: the server component reads auth(), and the route
// handler's signIn() redirects to the provider with the Auth.js cookie names.
func TestMigratedNextAuthAppBuildsAndServes(t *testing.T) {
	dist := authDistDir(t)
	root := t.TempDir()
	for rel, content := range map[string]string{
		"package.json":                            `{"name":"auth-app","private":true,"dependencies":{"next":"16.0.0","react":"19.0.0","next-auth":"5.0.0-beta.32","@auth/drizzle-adapter":"^1.11.3"}}`,
		"tsconfig.json":                           `{"compilerOptions":{"paths":{"@/*":["./src/*"]}}}`,
		"src/auth.ts":                             nextAuthMigrateConfig,
		"src/db/index.ts":                         `export const users = {}, accounts = {}, sessions = {}, verificationTokens = {}; export function getDb() { return {}; }`,
		"src/app/layout.tsx":                      `export default function L({ children }) { return <html><body>{children}</body></html>; }`,
		"src/app/page.tsx":                        "import { auth } from '../auth';\nimport { SignOut } from '../components/sign-out';\n\nexport default async function Home() {\n  const session = await auth();\n  return <main>{session ? <SignOut /> : <a href=\"/signin/popup\">Sign in</a>}</main>;\n}\n",
		"src/components/sign-out.tsx":             "import { signOut } from '../auth';\n\nexport function SignOut() {\n  async function out() {\n    'use server';\n    await signOut({ redirectTo: '/' });\n  }\n  return <form action={out}><button>Sign out</button></form>;\n}\n",
		"src/app/api/auth/[...nextauth]/route.ts": "import { handlers } from '../../../../auth';\n\nexport const { GET, POST } = handlers;\n",
		"src/app/signin/popup/route.ts":           "import { signIn } from '../../../auth';\n\nexport async function GET() {\n  await signIn('discord', { redirectTo: '/signin/done' });\n}\n",
		// The Auth.js adapter package, standing in for @auth/drizzle-adapter.
		"node_modules/@auth/drizzle-adapter/package.json": `{"name":"@auth/drizzle-adapter","type":"module","main":"index.js"}`,
		"node_modules/@auth/drizzle-adapter/index.js":     `export function DrizzleAdapter() { return {}; }`,
	} {
		writeFileRel(t, root, rel, content)
	}

	rep, err := migrate.ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Apply(rep, migrate.ApplyOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	migrated, err := os.ReadFile(filepath.Join(root, "src", "auth.ts"))
	if err != nil || strings.Contains(string(migrated), "next-auth") || !strings.Contains(string(migrated), "defineAuth") {
		t.Fatalf("src/auth.ts was not ported (err=%v):\n%s", err, migrated)
	}

	prepareMigratedRuntime(t, root)
	if err := os.Symlink(filepath.Dir(dist), filepath.Join(root, "node_modules", "@krate", "auth")); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	ver, err := VerifyMigration(root, rep.Routes, MigrationVerifyOptions{SkipCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	if !ver.BuildOK {
		t.Fatalf("the migrated app does not build: %s", ver.BuildError)
	}

	t.Setenv("AUTH_SECRET", "migrated-app-secret-with-plenty-of-entropy-0123456789")
	t.Setenv("AUTH_DISCORD_ID", "discord-client")
	t.Setenv("AUTH_DISCORD_SECRET", "discord-secret")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	resp, body := getRaw(t, base, "/", nil)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "Sign in") {
		t.Fatalf("/: status %d body %.200q, want the signed-out page", resp.StatusCode, body)
	}
	resp, _ = getRaw(t, base, "/api/signin/popup", nil)
	loc := resp.Header.Get("Location")
	if resp.StatusCode != 307 || !strings.HasPrefix(loc, "https://discord.com/api/oauth2/authorize?") || !strings.Contains(loc, "scope=identify+guilds") || !strings.Contains(loc, "client_id=discord-client") {
		t.Fatalf("signIn() in the route handler: %d Location %q", resp.StatusCode, loc)
	}
	cookies := strings.Join(resp.Header.Values("Set-Cookie"), "\n")
	if !strings.Contains(cookies, "authjs.state=") || !strings.Contains(cookies, "authjs.pkce.code_verifier=") {
		t.Fatalf("flow cookies must use the Auth.js names: %q", cookies)
	}
}
