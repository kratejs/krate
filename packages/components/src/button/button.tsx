import './button.css';
import { Slot } from '@krate/runtime';

export interface ButtonProps {
  children?: any;
  variant?: 'primary' | 'secondary' | 'outline' | 'ghost' | 'destructive' | 'link';
  size?: 'sm' | 'md' | 'lg' | 'icon';
  type?: 'button' | 'submit' | 'reset';
  disabled?: boolean;
  asChild?: boolean;
  class?: string;
  onClick?: (e: MouseEvent) => void;
}

export function Button(props: ButtonProps) {
  var variant = props.variant || "primary";
  var size = props.size || "md";
  var type = props.type || "button";

  var className = "krate-button krate-button-" + variant + " krate-button-" + size;
  if (props.class) className += " " + props.class;

  if (props.asChild) {
    return <Slot class={className}>{props.children}</Slot>;
  }

  return (
    <button
      class={className}
      type={type}
      disabled={props.disabled}
      onClick={props.onClick}
    >
      {props.children}
    </button>
  );
}
