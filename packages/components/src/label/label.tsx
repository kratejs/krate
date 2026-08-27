import './label.css';

export interface LabelProps {
  children?: any;
  htmlFor?: string;
  disabled?: boolean;
  required?: boolean;
}

export function Label(props: LabelProps) {
  return (
    <label
      class={"krate-label" + (props.disabled ? " krate-label-disabled" : "")}
      htmlFor={props.htmlFor || ""}
    >
      {props.children}
      {props.required ? <span class="krate-label-required">*</span> : null}
    </label>
  );
}
