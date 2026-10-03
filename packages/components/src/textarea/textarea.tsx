import './textarea.css';

export interface TextareaProps {
  value?: string;
  placeholder?: string;
  name?: string;
  id?: string;
  rows?: number;
  disabled?: boolean;
  required?: boolean;
  readonly?: boolean;
  class?: string;
  onInput?: (e: Event) => void;
}

export function Textarea(props: TextareaProps) {
  var className = "krate-textarea";
  if (props.class) className += " " + props.class;
  return (
    <textarea
      class={className}
      rows={props.rows || 3}
      placeholder={props.placeholder}
      name={props.name}
      id={props.id}
      disabled={props.disabled}
      required={props.required}
      readonly={props.readonly}
      onInput={props.onInput}
    >
      {props.value}
    </textarea>
  );
}
