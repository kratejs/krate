import { Toaster } from 'sonner';

export default function Layout({ children }: { children: any }) {
  return (
    <div>
      <nav><a id="to-b" href="/toastlayout/b/">b</a></nav>
      {children}
      <Toaster />
    </div>
  );
}
