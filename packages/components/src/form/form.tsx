import './form.css';
import { createContext } from '@krate/runtime';

// What a Form.Field tells the control inside it, so the description and error
// are announced with the control and an error marks it invalid.
const FormFieldCtx = createContext<{ describedBy: string; invalid: boolean }>({ describedBy: '', invalid: false });

export interface FormProps {
  action?: string;
  method?: 'get' | 'post';
  class?: string;
  /** Alias of `class`. */
  className?: string;
  id?: string;
  children?: any;
}

/**
 * Form container. Uses native submission and validation, so it needs no
 * JavaScript. Provide `action`/`method` to submit to a server route.
 */
export function Form(props: FormProps) {
  return (
    <form class={'krate-form' + (props.class ? ' ' + props.class : '') + (props.className ? ' ' + props.className : '')} id={props.id} action={props.action} method={props.method}>
      {props.children}
    </form>
  );
}

export interface FormFieldProps {
  label?: string;
  description?: string;
  error?: string;
  required?: boolean;
  /**
   * Base for the ids of the description and error text. Defaults to a slug of
   * the label, so give fields with the same label different ids.
   */
  id?: string;
  class?: string;
  /** Alias of `class`. */
  className?: string;
  children?: any;
}

/**
 * A labelled field. Its description and error are linked to the control inside
 * with `aria-describedby`, and an error sets `aria-invalid` on it.
 *
 * @deprecated Use `Form.Field` instead.
 */
export function FormField(props: FormFieldProps) {
  var base = "krate-field-" + (props.id || (props.label || "").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, ""));
  var descId = props.description ? base + "-description" : "";
  var errId = props.error ? base + "-error" : "";
  var describedBy = (descId && errId ? descId + " " + errId : descId + errId);
  return (
    <FormFieldCtx.Provider value={{ describedBy: describedBy, invalid: !!props.error }}>
      <div class={"krate-form-field" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}>
        <label class="krate-form-field-label">
          <span class="krate-form-field-text">
            {props.label}
            {props.required ? <span class="krate-form-required" aria-hidden="true"> *</span> : null}
          </span>
          <span class="krate-form-field-control">{props.children}</span>
        </label>
        {props.description ? <p class="krate-form-field-description" id={descId}>{props.description}</p> : null}
        {props.error ? (
          <p class="krate-form-field-error" id={errId} role="alert">
            {props.error}
          </p>
        ) : null}
      </div>
    </FormFieldCtx.Provider>
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
  var field = FormFieldCtx.use();
  return (
    <input
      class="krate-input"
      aria-describedby={field.describedBy || undefined}
      aria-invalid={field.invalid ? "true" : undefined}
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
  var field = FormFieldCtx.use();
  return (
    <textarea
      class="krate-textarea"
      aria-describedby={field.describedBy || undefined}
      aria-invalid={field.invalid ? "true" : undefined}
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
