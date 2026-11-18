package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ddAuthDecl = `import type { Role, Tier } from '@dd/shared';
import type { DefaultSession } from 'next-auth';

declare module 'next-auth' {
  interface Session {
    user: {
      id: string;
      role: Role;
      tier: Tier;
      discordId: string;
      credits: number;
      banned: boolean;
      /** Early access during coming-soon mode (admin-granted). */
      comingSoonAccess?: boolean;
    } & DefaultSession['user'];
  }
}
`

const ddAuthDeclWant = `import type { Role, Tier } from '@dd/shared';

declare module '@krate/auth' {
  interface SessionUserExtensions {
    id: string;
    role: Role;
    tier: Tier;
    discordId: string;
    credits: number;
    banned: boolean;
    /** Early access during coming-soon mode (admin-granted). */
    comingSoonAccess?: boolean;
  }
}
`

func TestPortAuthDeclarationGolden(t *testing.T) {
	out, why := portAuthDeclaration(ddAuthDecl)
	if why != "" || out != ddAuthDeclWant {
		t.Fatalf("why=%q\n%s", why, out)
	}
	if data, err := os.ReadFile("/home/dev/Repos/DigitalDesigns/packages/web/src/types/next-auth.d.ts"); err == nil {
		if out, why := portAuthDeclaration(string(data)); why != "" || out != ddAuthDeclWant {
			t.Fatalf("real file: why=%q\n%s", why, out)
		}
	}
}

func TestPortAuthDeclarationUserAndBareImport(t *testing.T) {
	out, why := portAuthDeclaration("import 'next-auth';\ndeclare module \"next-auth\" {\n  interface User { role: 'a' | 'b' }\n  interface Session { user: { role: 'a' | 'b' } }\n}\nexport {};\n")
	if why != "" {
		t.Fatal(why)
	}
	for _, want := range []string{"interface SessionUserExtensions {\n    role: 'a' | 'b'", "interface UserExtensions {\n    role: 'a' | 'b'", "export {};"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "next-auth") {
		t.Errorf("next-auth left in\n%s", out)
	}
}

func TestPortAuthDeclarationRejectsUnknownShapes(t *testing.T) {
	for name, src := range map[string]string{
		"jwt":         "declare module 'next-auth/jwt' { interface JWT { role: string } }\n",
		"conditional": "declare module 'next-auth' { interface Session { user: Foo extends Bar ? A : B } }\n",
		"extends":     "declare module 'next-auth' { interface Session extends X { user: { a: 1 } } }\n",
		"other key":   "declare module 'next-auth' { interface Session { token: string } }\n",
		"other iface": "declare module 'next-auth' { interface Profile { a: 1 } }\n",
		"extra code":  "const x = 1;\ndeclare module 'next-auth' { interface Session { user: { a: 1 } } }\n",
	} {
		if out, why := portAuthDeclaration(src); why == "" {
			t.Errorf("%s: converted unexpectedly:\n%s", name, out)
		}
	}
}

func TestNextAuthAugmentationIsRenamedAndRollsBack(t *testing.T) {
	root := nextAuthProject(t, nextAuthFixtureConfig)
	writeNextFile(t, root, "src/types/next-auth.d.ts", ddAuthDecl)
	rep, err := ScanNextWithOptions(root, ScanOptions{KrateVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	f := findPlan(rep, "src/types/krate-auth.d.ts")
	if f == nil || string(f.Content) != ddAuthDeclWant {
		t.Fatalf("converted declaration: %+v", f)
	}
	if !containsString(rep.MoveAside, "src/types/next-auth.d.ts") {
		t.Errorf("the original is not moved aside: %v", rep.MoveAside)
	}
	for _, m := range rep.Manual {
		if m.Source != "package.json" {
			t.Errorf("unexpected manual item: %+v", m)
		}
	}
	if _, ok := depOf(pkgJSON(t, rep), "next-auth"); ok {
		t.Error("next-auth stays although nothing imports it")
	}
	res, err := Apply(rep, ApplyOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "src/types/next-auth.d.ts")); !os.IsNotExist(err) {
		t.Errorf("the original declaration is still in place: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "src/types/krate-auth.d.ts")); string(got) != ddAuthDeclWant {
		t.Errorf("written declaration:\n%s", got)
	}
	man, _ := os.ReadFile(filepath.Join(res.BackupDir, "manifest.json"))
	if !strings.Contains(string(man), "src/types/krate-auth.d.ts") || !strings.Contains(string(man), "src/types/next-auth.d.ts") {
		t.Errorf("manifest does not record the rename:\n%s", man)
	}
	if err := Rollback(res.BackupDir); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "src/types/next-auth.d.ts")); string(got) != ddAuthDecl {
		t.Errorf("original not restored:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(root, "src/types/krate-auth.d.ts")); !os.IsNotExist(err) {
		t.Errorf("the converted file survived the rollback: %v", err)
	}
}
