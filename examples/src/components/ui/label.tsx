import { cn } from '@krate/runtime';

export function Label({ className, htmlFor, children }: any) {
  return <label htmlFor={htmlFor} className={cn('text-sm font-medium text-slate-200', className)}>{children}</label>;
}
