import './password-toggle-field.css';
import { createSignal } from '@krate/runtime';

export interface PasswordToggleFieldProps {
  placeholder?: string;
  value?: string;
  disabled?: boolean;
  id?: string;
  name?: string;
  showToggle?: boolean;
}

export function PasswordToggleField(props: PasswordToggleFieldProps) {
  var [visible, setVisible] = createSignal(false);

  function toggle() {
    setVisible(!visible());
  }

  return (
    <div class="krate-password-toggle-field">
      <input
        class="krate-password-toggle-input"
        type={visible() ? "text" : "password"}
        placeholder={props.placeholder || ""}
        value={props.value || ""}
        disabled={props.disabled}
        id={props.id || ""}
        name={props.name || ""}
      />
      <button
        class="krate-password-toggle-button"
        type="button"
        onClick={toggle}
        aria-label={visible() ? "Hide password" : "Show password"}
      >
        {visible() ? (
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M9.88 9.88a3 3 0 1 0 4.24 4.24"/>
            <path d="M10.73 5.08A10.43 10.43 0 0 1 12 5c7 0 10 7 10 7a13.16 13.16 0 0 1-1.67 2.68"/>
            <path d="M6.61 6.61A13.526 13.526 0 0 0 2 12s3 7 10 7a9.74 9.74 0 0 0 5.39-1.61"/>
            <line x1="2" x2="22" y1="2" y2="22"/>
          </svg>
        ) : (
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M2 12s3-7 10-7 10 7 10 7-3 7-10 7-10-7-10-7Z"/>
            <circle cx="12" cy="12" r="3"/>
          </svg>
        )}
      </button>
    </div>
  );
}
