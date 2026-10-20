import { cn } from '@krate/runtime';

export function Badge({ className, children }: any) {
  return (
    <span className={cn('inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold', className)}>
      {children}
    </span>
  );
}