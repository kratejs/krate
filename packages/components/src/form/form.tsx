import './form.css';

export interface FormProps {
  action?: string;
  method?: 'get' | 'post';
  className?: string;
  children?: any;
}

/**
 * Form container. Uses native submission and validation, so it needs no
 * JavaScript. Provide `action`/`method` to submit to a server route.
 */
export function Form(props: FormProps) {
  return (
    <form class={'krate-form' + (props.className ? ' ' + props.className : '')} action={props.action} method={props.method}>
      {props.children}
    </form>
  );
}

export interface FormFieldProps {
  label?: string;
  description?: string;
  error?: string;
  required?: boolean;
  children?: any;
}

/** @deprecated Use `Form.Field` instead. */
export function FormField(props: FormFieldProps) {
  return (
    <div class="krate-form-field">
      <label class="krate-form-field-label">
        <span class="krate-form-field-text">
          {props.label}
          {props.required ? <span class="krate-form-required" aria-hidden="true"> *</span> : null}
        </span>
        <span class="krate-form-field-control">{props.children}</span>
      </label>
      {props.description ? <p class="krate-form-field-description">{props.description}</p> : null}
      {props.error ? (
        <p class="krate-form-field-error" role="alert">
          {props.error}
        </p>
      ) : null}
    </div>
  );
}

export interface FormInputProps {
  type?: string;
  placeholder?: string;
  defaultValue?: string;
  disabled?: boolean;
  required?: boolean;
  id?: string;
  name?: string;
}

/** @deprecated Use `Form.Input` instead. */
export function FormInput(props: FormInputProps) {
  return (
    <input
      class="krate-input"
      type={props.type || 'text'}
      placeholder={props.placeholder}
      value={props.defaultValue}
      disabled={props.disabled}
      required={props.required}
      id={props.id}
      name={props.name}
    />
  );
}

export interface FormSubmitProps {
  variant?: 'primary' | 'secondary';
  children?: any;
}

/** Submit button for a {@link Form}. */
/** @deprecated Use `Form.Submit` instead. */
export function FormSubmit(props: FormSubmitProps) {
  return (
    <button class={'krate-button krate-button-' + (props.variant || 'primary')} type="submit">
      {props.children}
    </button>
  );
}

export interface FormTextareaProps {
  placeholder?: string;
  defaultValue?: string;
  disabled?: boolean;
  required?: boolean;
  rows?: number;
  id?: string;
  name?: string;
}

/** @deprecated Use `Form.Textarea` instead. */
export function FormTextarea(props: FormTextareaProps) {
  return (
    <textarea
      class="krate-textarea"
      placeholder={props.placeholder}
      value={props.defaultValue}
      disabled={props.disabled}
      required={props.required}
      rows={props.rows}
      id={props.id}
      name={props.name}
    />
  );
}

export interface Form {
  Field: typeof FormField;
  Input: typeof FormInput;
  Textarea: typeof FormTextarea;
  Submit: typeof FormSubmit;
}

Form.Field = FormField;
Form.Input = FormInput;
Form.Textarea = FormTextarea;
Form.Submit = FormSubmit;
