import './label.css';

export interface LabelProps {
  children?: any;
  htmlFor?: string;
  disabled?: boolean;
  required?: boolean;
  /** Extra classes for the root element. */
  class?: string;
  /** Alias of `class`. */
  className?: string;
  id?: string;
}

export function Label(props: LabelProps) {
  return (
    <label
      class={"krate-label" + (props.disabled ? " krate-label-disabled" : "") + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}
      id={props.id}
      htmlFor={props.htmlFor || ""}
    >
      {props.children}
      {props.required ? <span class="krate-label-required">*</span> : null}
    </label>
  );
}
