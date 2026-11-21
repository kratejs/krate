package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBrowserAsChildServerChild covers a shadcn account menu: a client Radix
// trigger with asChild whose child is a directive-less server Button holding
// a client Avatar. The trigger must clone the button (merging its handlers
// and ARIA props), and the Avatar must render inside the same island tree. A
// top-level asChild client component (Pressable) gets the same treatment.
func TestBrowserAsChildServerChild(t *testing.T) {
	t.Parallel()
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser checks")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	script, err := filepath.Abs(filepath.Join("testdata", "browser_aschild.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	radix, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "examples", "node_modules", "radix-ui"))
	if err == nil {
		radix, err = filepath.EvalSymlinks(radix)
	}
	if err != nil {
		requireE2E(t, "radix-ui is not installed for the examples (run pnpm install): %v", err)
	}
	b, root := scProject(t, map[string]string{
		"src/components/dropdown-menu.tsx": `'use client';
import { DropdownMenu as P } from 'radix-ui';
export function DropdownMenu(props: any) {
  return <P.Root data-slot="dropdown-menu" {...props} />;
}
export function DropdownMenuTrigger(props: any) {
  return <P.Trigger data-slot="dropdown-menu-trigger" {...props} />;
}
export function DropdownMenuContent({ sideOffset = 4, ...props }: any) {
  return <P.Portal><P.Content data-slot="dropdown-menu-content" sideOffset={sideOffset} {...props} /></P.Portal>;
}
export function DropdownMenuItem(props: any) {
  return <P.Item data-slot="dropdown-menu-item" {...props} />;
}
`,
		"src/components/button.tsx": `import { Slot } from 'radix-ui';
export function Button({ asChild = false, variant = 'default', ...props }: any) {
  const Comp = asChild ? Slot.Root : 'button';
  return <Comp data-slot="button" data-variant={variant} {...props} />;
}
`,
		"src/components/avatar.tsx": `'use client';
import { useEffect, useState } from 'react';
export function Avatar({ initials }: { initials: string }) {
  const [ready, setReady] = useState(false);
  useEffect(() => setReady(true), []);
  return <span id="avatar" data-ready={ready ? 'yes' : 'no'}>{initials}</span>;
}
`,
		"src/components/user-menu.tsx": `import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from './dropdown-menu';
import { Button } from './button';
import { Avatar } from './avatar';
export function AccountMenu({ name }: { name: string }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" aria-label="Account menu" id="account">
          <Avatar initials="AB" />
          <span>{name}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem id="item-profile">Profile</DropdownMenuItem>
        <DropdownMenuItem>Sign out</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
`,
		"src/components/pressable.tsx": `'use client';
import { useState } from 'react';
import { Slot } from 'radix-ui';
export function Pressable({ asChild, ...props }: any) {
  const [n, setN] = useState(0);
  const Comp = asChild ? Slot.Root : 'span';
  return <Comp data-pressed={String(n)} onClick={() => setN(n + 1)} {...props} />;
}
`,
		"src/pages/index.tsx": `import { AccountMenu } from '../components/user-menu';
import { Pressable } from '../components/pressable';
import { Button } from '../components/button';
export default async function Page() {
  await Promise.resolve();
  return <main><AccountMenu name="Ada" /><Pressable asChild><Button id="press">press</Button></Pressable></main>;
}
`,
	})
	withPreact(t, root)
	if err := os.Symlink(radix, filepath.Join(root, "node_modules", "radix-ui")); err != nil {
		t.Skipf("cannot link radix-ui: %v", err)
	}
	e2eEnv(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if out, err := captureBuild(b); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	status, _, html := getBody(t, base+"/")
	if status != 200 || !strings.Contains(html, `aria-haspopup="menu"`) {
		t.Fatalf("the server must render the trigger props onto the server Button (status %d):\n%s", status, html)
	}
	if !strings.Contains(html, `data-pressed="0"`) {
		t.Fatalf("a top-level asChild client component must merge its props into the server Button:\n%s", html)
	}
	if strings.Contains(html, `data-export="Avatar"`) {
		t.Fatalf("Avatar must render inside the menu's island tree, not as a nested island:\n%s", html)
	}
	out, err := exec.Command("node", script, browser, base).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser checks failed: %v\n%s", err, out)
	}
}
