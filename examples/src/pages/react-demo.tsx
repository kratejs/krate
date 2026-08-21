import { useState, useEffect, useRef, useCallback, forwardRef } from 'react';
import { Popover } from "radix-ui";
import Button from '../../components/ui/button';
// import { Slot } from "@radix-ui/react-slot" // If you haven't installed radix yet, swap <Slot> out for "button" below
// import { cva, type VariantProps } from "class-variance-authority"
// import { cn } from "../../lib/utils"

// const buttonVariants = cva(
//   "inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50",
//   {
//     variants: {
//       variant: {
//         default: "bg-primary text-primary-foreground shadow hover:bg-primary/90 bg-blue-600 text-white",
//         destructive: "bg-destructive text-destructive-foreground shadow-sm hover:bg-destructive/90 bg-red-600 text-white",
//         outline: "border border-input bg-background shadow-sm hover:bg-accent hover:text-accent-foreground border-gray-300",
//       },
//       size: {
//         default: "h-9 px-4 py-2",
//         sm: "h-8 rounded-md px-3 text-xs",
//         lg: "h-10 rounded-md px-8",
//         icon: "h-9 w-9",
//       },
//     },
//     defaultVariants: {
//       variant: "default",
//       size: "default",
//     },
//   }
// )

// export interface ButtonProps
//   extends React.ButtonHTMLAttributes<HTMLButtonElement>,
//     VariantProps<typeof buttonVariants> {
//   asChild?: boolean
// }

// THIS LINE IS CRITICAL FOR YOUR COMPILER TEST:

// import { type ClassValue, clsx } from "clsx"
// import { twMerge } from "tailwind-merge"

// export function cn(...inputs: ClassValue[]) {
//   return twMerge(clsx(inputs))
// }

interface TimerProps { initial?: number };

// const PopoverDemo = () => (
// 	<Popover.Root>
// 		<Popover.Trigger>More info</Popover.Trigger>
// 		<Popover.Portal>
// 			<Popover.Content>
// 				Some more info…
// 				<Popover.Arrow />
// 			</Popover.Content>
// 		</Popover.Portal>
// 	</Popover.Root>
// );

function PopoverDemo() {
    return (
    <Popover.Root>
		<Popover.Trigger>More info</Popover.Trigger>
		<Popover.Portal>
			<Popover.Content>
				Some more info…
				<Popover.Arrow />
			</Popover.Content>
		</Popover.Portal>
	</Popover.Root>
    )
}

function TimerDisplay(props: TimerProps) {
  const [seconds, setSeconds] = useState(0);
  const intervalRef = useRef<NodeJS.Timeout | null>(null);

  useEffect(() => {
    intervalRef.current = setInterval(() => {
      setSeconds((prev: number) => prev + 1);
    }, 1000);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, []);

  return (
    <div class="counter">
      <span class="count">{seconds}</span>
      <p>Seconds elapsed: {seconds}</p>
    </div>
  );
}

interface GreetingProps { initial?: string }

function Greeting(props: GreetingProps) {
  const [name, setName] = useState(props.initial || 'World');

  return (
    <div class="card">
      <h1>React Compatibility Demo</h1>
      <p>This page uses React-compatible syntax (<code>useState</code>, <code>useEffect</code>, <code>useRef</code>) that gets rewritten to krate signals and effects at compile time.</p>
      <Icon name="lucide:heart" />
      <div class="counter">
        <span class="count">{name}</span>
        <button onClick={() => setName('Krate')}>Set Name</button>
        <button onClick={() => setName('React')}>Set Name</button>
        <Button />
      </div>
      <TimerDisplay initial={0} />
      <PopoverDemo />
    </div>
  );
}

export default Greeting;
