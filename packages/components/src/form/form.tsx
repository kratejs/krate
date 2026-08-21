import './form.css';
import { createSignal } from '@krate/runtime';

export interface FormProps {
  children?: any;
  onSubmit?: (e: Event) => void;
  className?: string;
}

export default function Form(props: FormProps) {
  var className = "krate-form" + (props.className ? " " + props.className : "");

  function handleSubmit(e: Event) {
    e.preventDefault();
    if (props.onSubmit) {
      props.onSubmit(e);
    }
  }

  return (
    <form class={className} onSubmit={handleSubmit}>
      {props.children}
    </form>
  );
}

export interface FormFieldProps {
  children?: any;
  label?: string;
  description?: string;
  error?: string;
  required?: boolean;
}

export function FormField(props: FormFieldProps) {
  return (
    <div class="krate-form-field">
      {props.label ? (
        <label class="krate-form-label">
          {props.label}
          {props.required ? <span class="krate-form-required">*</span> : null}
        </label>
      ) : null}
      <div class="krate-form-control">
        {props.children}
      </div>
      {props.description ? <p class="krate-form-description">{props.description}</p> : null}
      {props.error ? <p class="krate-form-error">{props.error}</p> : null}
    </div>
  );
}

export interface FormInputProps {
  type?: string;
  placeholder?: string;
  value?: string;
  disabled?: boolean;
  id?: string;
  name?: string;
}

export function FormInput(props: FormInputProps) {
  var type = props.type || "text";
  return (
    <input
      class="krate-form-input"
      type={type}
      placeholder={props.placeholder || ""}
      value={props.value || ""}
      disabled={props.disabled}
      id={props.id || ""}
      name={props.name || ""}
    />
  );
}

export interface FormTextareaProps {
  placeholder?: string;
  value?: string;
  disabled?: boolean;
  rows?: number;
  id?: string;
  name?: string;
}

export function FormTextarea(props: FormTextareaProps) {
  return (
    <textarea
      class="krate-form-textarea"
      placeholder={props.placeholder || ""}
      disabled={props.disabled}
      rows={props.rows || 4}
      id={props.id || ""}
      name={props.name || ""}
    >
      {props.value || ""}
    </textarea>
  );
}
