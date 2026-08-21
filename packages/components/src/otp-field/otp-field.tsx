import './otp-field.css';
import { createSignal, onMount, onCleanup } from '@krate/runtime';

export interface OTPFieldProps {
  length?: number;
  value?: string;
  onValueChange?: (value: string) => void;
  disabled?: boolean;
  autoFocus?: boolean;
  onComplete?: (value: string) => void;
}

export default function OTPField(props: OTPFieldProps) {
  var length = props.length || 6;
  var [values, setValues] = createSignal<string[]>(["", "", "", "", "", ""]);

  function handleInputAt(index: number, e: Event) {
    var target = e.target as HTMLInputElement;
    var val = target.value;
    if (val.length > 1) {
      val = val.charAt(val.length - 1);
    }
    if (!/^[0-9a-zA-Z]$/.test(val)) {
      val = "";
    }
    setValues(function (prev) {
      var next = prev.slice();
      next[index] = val;
      return next;
    });
    if (val && index < length - 1) {
      var allInputs = (e.target as HTMLElement).closest("[data-krate-otp-field]")?.querySelectorAll(".krate-otp-field-input") as NodeListOf<HTMLElement>;
      if (allInputs && allInputs[index + 1]) {
        allInputs[index + 1].focus();
      }
    }
    var currentValues = values();
    currentValues[index] = val;
    var full = currentValues.join("");
    if (props.onValueChange) {
      props.onValueChange(full);
    }
    if (full.length === length && full.replace(/,/g, "").length === length) {
      if (props.onComplete) {
        props.onComplete(full);
      }
    }
  }

  function handleKeyDownAt(index: number, e: KeyboardEvent) {
    if (e.key === "Backspace") {
      var currentValues = values();
      if (!currentValues[index] && index > 0) {
        var allInputs = (e.target as HTMLElement).closest("[data-krate-otp-field]")?.querySelectorAll(".krate-otp-field-input") as NodeListOf<HTMLElement>;
        if (allInputs && allInputs[index - 1]) {
          allInputs[index - 1].focus();
        }
        setValues(function (prev) {
          var next = prev.slice();
          next[index - 1] = "";
          return next;
        });
      } else {
        setValues(function (prev) {
          var next = prev.slice();
          next[index] = "";
          return next;
        });
      }
      e.preventDefault();
    } else if (e.key === "ArrowLeft" && index > 0) {
      var allInputs = (e.target as HTMLElement).closest("[data-krate-otp-field]")?.querySelectorAll(".krate-otp-field-input") as NodeListOf<HTMLElement>;
      if (allInputs && allInputs[index - 1]) {
        allInputs[index - 1].focus();
      }
    } else if (e.key === "ArrowRight" && index < length - 1) {
      var allInputs = (e.target as HTMLElement).closest("[data-krate-otp-field]")?.querySelectorAll(".krate-otp-field-input") as NodeListOf<HTMLElement>;
      if (allInputs && allInputs[index + 1]) {
        allInputs[index + 1].focus();
      }
    }
  }

  function handleInputEvent(e: Event) {
    var index = Number((e.target as HTMLElement).getAttribute("data-otp-index"));
    handleInputAt(index, e);
  }

  function handleKeyDownEvent(e: Event) {
    var index = Number((e.target as HTMLElement).getAttribute("data-otp-index"));
    handleKeyDownAt(index, e as KeyboardEvent);
  }

  function handlePaste(e: ClipboardEvent) {
    e.preventDefault();
    var root = (e.target as HTMLElement).closest("[data-krate-otp-field]") as HTMLElement;
    var len = root ? Number(root.getAttribute("data-length")) : 6;
    var text = (e.clipboardData || (window as any).clipboardData).getData("text").replace(/[^0-9a-zA-Z]/g, "").slice(0, len);
    var chars = text.split("");
    setValues(function (prev) {
      var next = prev.slice();
      for (var i = 0; i < len; i++) {
        next[i] = chars[i] || "";
      }
      return next;
    });
    var allInputs = root?.querySelectorAll(".krate-otp-field-input") as NodeListOf<HTMLElement>;
    var focusIdx = Math.min(chars.length, len - 1);
    if (allInputs && allInputs[focusIdx]) {
      allInputs[focusIdx].focus();
    }
  }

  var inputElements = [];
  for (var i = 0; i < length; i++) {
    inputElements.push(
      <input
        class="krate-otp-field-input"
        type="text"
        inputMode="numeric"
        maxLength={1}
        data-otp-index={String(i)}
        disabled={props.disabled}
        onInput={handleInputEvent}
        onKeyDown={handleKeyDownEvent}
      />
    );
  }

  return (
    <div class="krate-otp-field" data-krate-otp-field="true" data-length={length} onPaste={handlePaste}>
      {inputElements}
    </div>
  );
}
