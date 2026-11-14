import { Toaster } from 'sonner';

async function loadSession() {
  return { user: 'ada' };
}

export default async function Layout({ children }: { children: any }) {
  const session = await loadSession();
  return (
    <div data-user={session.user}>
      <nav><a id="to-b" href="/asynctoast/b/">b</a></nav>
      {children}
      <Toaster />
    </div>
  );
}
