import './input.css';

export interface InputProps {
  type?: string;
  value?: string;
  placeholder?: string;
  name?: string;
  id?: string;
  disabled?: boolean;
  required?: boolean;
  readonly?: boolean;
  autocomplete?: string;
  class?: string;
  onInput?: (e: Event) => void;
}

export function Input(props: InputProps) {
  var type = props.type || "text";
  var className = "krate-input";
  if (props.class) className += " " + props.class;
  return (
    <input
      class={className}
      type={type}
      value={props.value}
      placeholder={props.placeholder}
      name={props.name}
      id={props.id}
      disabled={props.disabled}
      required={props.required}
      readonly={props.readonly}
      autocomplete={props.autocomplete}
      onInput={props.onInput}
    />
  );
}
