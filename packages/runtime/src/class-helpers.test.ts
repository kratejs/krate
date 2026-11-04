import { describe, it, expect } from 'vitest';
import { cn, twMerge, clsx } from './class-helpers';

// Each case: input classes, expected merged output (later wins).
const same = (a: string, b: string): void => {
  expect(twMerge(a, b)).toBe(b);
};
const apart = (a: string, b: string): void => {
  expect(twMerge(a, b).split(' ').sort()).toEqual([a, b].sort());
};

describe('twMerge numeric and arbitrary values', () => {
  it('collapses multi-digit values', () => {
    same('duration-700', 'duration-500');
    same('delay-150', 'delay-300');
    same('duration-75', 'duration-1000');
    expect(cn('duration-700', 'duration-500')).toBe('duration-500');
  });
  it('collapses arbitrary and variable values', () => {
    same('duration-500', 'duration-[2s]');
    same('duration-[2s]', 'duration-(--d)');
    same('p-4', 'p-[3px]');
    same('w-1/2', 'w-[200px]');
    same('mt-2', 'mt-(--gap)');
  });
  it('handles decimals, fractions, negatives, modifiers and important', () => {
    same('p-0.5', 'p-12');
    same('w-1/2', 'w-1/3');
    same('-mt-2', 'mt-4');
    same('mt-4', '-mt-2');
    same('bg-red-500/50', 'bg-blue-500');
    same('bg-red-500', 'bg-blue-500/20');
    same('!p-2', '!p-4');
    same('p-2!', 'p-4!');
    same('!p-2', 'p-4!');
    apart('!p-2', 'p-4');
  });
  it('scopes by variant chain', () => {
    apart('hover:duration-500', 'duration-700');
    same('hover:duration-500', 'hover:duration-700');
    same('md:hover:p-2', 'hover:md:p-4');
    apart('md:p-2', 'p-4');
    same('[&>*]:p-2', '[&>*]:p-4');
    apart('[&>*]:p-2', '[&_a]:p-4');
  });
  it('keeps unrelated and unknown classes', () => {
    expect(twMerge('px-2', 'py-2')).toBe('px-2 py-2');
    expect(twMerge('krate-card', 'krate-card-title', 'krate-card')).toBe('krate-card-title krate-card');
    expect(twMerge('foo-bar', 'foo-baz')).toBe('foo-bar foo-baz');
    expect(twMerge('a b', 'c a')).toBe('b c a');
  });
});

describe('twMerge groups', () => {
  it('spacing and sizing', () => {
    same('p-2', 'p-4');
    same('px-2', 'px-4');
    apart('pt-2', 'pb-2');
    expect(twMerge('px-2', 'py-2', 'p-4')).toBe('p-4');
    expect(twMerge('p-4', 'px-2')).toBe('p-4 px-2');
    same('m-2', 'm-8');
    same('gap-2', 'gap-10');
    apart('gap-x-2', 'gap-y-2');
    expect(twMerge('gap-x-2', 'gap-4')).toBe('gap-4');
    same('w-10', 'w-full');
    same('h-10', 'h-screen');
    apart('w-10', 'h-10');
    expect(twMerge('w-4', 'h-4', 'size-8')).toBe('size-8');
    same('min-w-0', 'min-w-[10px]');
    apart('min-w-0', 'max-w-0');
    same('z-10', 'z-50');
    same('opacity-50', 'opacity-100');
  });
  it('text size versus color', () => {
    apart('text-sm', 'text-red-500');
    same('text-sm', 'text-lg');
    same('text-sm', 'text-2xl');
    same('text-red-500', 'text-blue-500');
    same('text-red-500', 'text-[#fff]');
    same('text-sm', 'text-[14px]');
    apart('text-[14px]', 'text-[#fff]');
    apart('text-sm', 'text-center');
    same('text-left', 'text-center');
    expect(twMerge('leading-6', 'text-lg')).toBe('text-lg');
    same('leading-6', 'leading-tight');
    same('tracking-tight', 'tracking-wide');
  });
  it('background', () => {
    same('bg-red-500', 'bg-blue-500');
    apart('bg-red-500', 'bg-cover');
    apart('bg-red-500', 'bg-center');
    same('bg-cover', 'bg-contain');
    same('bg-center', 'bg-top');
    apart('bg-cover', 'bg-center');
    same('bg-red-500', 'bg-[#fff]');
    same('bg-cover', 'bg-[length:10px_10px]');
    apart('bg-no-repeat', 'bg-cover');
  });
  it('border', () => {
    apart('border-2', 'border-red-500');
    same('border-2', 'border-4');
    same('border', 'border-2');
    same('border-red-500', 'border-blue-500');
    apart('border-solid', 'border-2');
    same('border-solid', 'border-dashed');
    apart('border-t-2', 'border-b-2');
    same('border-t-2', 'border-t-4');
    apart('border-t-2', 'border-t-red-500');
    expect(twMerge('border-t-2', 'border-x-2', 'border-4')).toBe('border-4');
    same('border-[3px]', 'border-2');
    apart('border-[3px]', 'border-[#fff]');
    same('rounded', 'rounded-lg');
    same('rounded-t-md', 'rounded-t-lg');
    apart('rounded-t-md', 'rounded-b-md');
    expect(twMerge('rounded-tl-md', 'rounded-t-lg')).toBe('rounded-t-lg');
  });
  it('shadow, font, motion, layout', () => {
    same('shadow-sm', 'shadow-lg');
    same('shadow', 'shadow-md');
    apart('shadow-md', 'shadow-red-500');
    same('shadow-red-500', 'shadow-blue-500');
    same('font-bold', 'font-light');
    apart('font-bold', 'font-sans');
    same('font-sans', 'font-mono');
    same('transition', 'transition-colors');
    same('transition-all', 'transition-none');
    same('ease-in', 'ease-out');
    same('ease-in', 'ease-[cubic-bezier(0.4,0,0.2,1)]');
    same('animate-spin', 'animate-pulse');
    same('animate-spin', 'animate-none');
    same('grid-cols-2', 'grid-cols-12');
    same('grid-cols-2', 'grid-cols-[1fr_2fr]');
    same('flex', 'block');
    same('hidden', 'flex');
    apart('flex', 'flex-col');
    same('flex-row', 'flex-col');
    same('absolute', 'relative');
    same('ring-2', 'ring-4');
    apart('ring-2', 'ring-red-500');
    same('outline-2', 'outline-4');
    apart('outline-2', 'outline-red-500');
  });
  it('arbitrary properties', () => {
    same('[mask-type:alpha]', '[mask-type:luminance]');
    apart('[mask-type:alpha]', '[color:red]');
  });
  it('works through clsx and cva-style inputs', () => {
    expect(cn('p-2', false && 'p-3', ['px-4', { 'px-6': true }])).toBe('p-2 px-6');
    expect(clsx('a', null, 'b')).toBe('a b');
  });
});
