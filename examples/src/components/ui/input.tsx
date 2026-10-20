import { cn } from '@krate/runtime';

export function Input({ className, placeholder, id, value, onChange, type = 'text' }: any) {
  return (
    <input
      id={id}
      type={type}
      value={value}
      placeholder={placeholder}
      onChange={onChange}
      className={cn(
        'h-9 w-full rounded-md border border-slate-700 bg-slate-950 px-3 text-sm text-slate-100 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-400',
        className
      )}
    />
  );
}