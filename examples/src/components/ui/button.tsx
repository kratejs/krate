import { Slot, cva, cn } from '@krate/runtime';

const buttonVariants = cva(
  'inline-flex items-center justify-center gap-2 rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-400 focus-visible:ring-offset-2 focus-visible:ring-offset-slate-950 disabled:pointer-events-none disabled:opacity-50',
  {
    variants: {
      variant: {
        default: 'bg-indigo-600 text-white shadow hover:bg-indigo-500',
        secondary: 'bg-slate-700 text-slate-100 hover:bg-slate-600',
        outline: 'border border-slate-600 bg-transparent text-slate-100 hover:bg-slate-800',
        ghost: 'bg-transparent text-slate-200 hover:bg-slate-800',
        destructive: 'bg-red-600 text-white shadow-sm hover:bg-red-500',
        link: 'bg-transparent text-indigo-300 underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-9 px-4 py-2',
        sm: 'h-8 rounded-md px-3 text-xs',
        lg: 'h-10 rounded-md px-8',
        icon: 'h-9 w-9',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  }
);

export function Button({ className, variant = 'default', size = 'default', ...props }: any) {
  const Comp = props.asChild ? Slot : 'button';
  return (
    <Comp class={cn(buttonVariants({ variant, size }), className)} onClick={props.onClick}>
      {props.children}
    </Comp>
  );
}

export default Button;
