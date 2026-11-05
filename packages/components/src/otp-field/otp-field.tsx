import './otp-field.css';

export interface OTPFieldProps {
  length?: number;
  /** Initial code. The field is uncontrolled: it owns the digits after mount. */
  value?: string;
  /** Submits the full code under this name when the field is inside a form. */
  name?: string;
  onValueChange?: (value: string) => void;
  disabled?: boolean;
  /** Accessible name for the group; each digit is named "<label> digit N of M". */
  label?: string;
  autoFocus?: boolean;
  onComplete?: (value: string) => void;
}

/**
 * One-time-code input: a box per digit. The digits live in the inputs
 * themselves, so typing, Backspace, paste and SMS autofill all edit what is on
 * screen. Arrow keys, Home and End move between boxes.
 */
export function OTPField(props: OTPFieldProps) {
  var length = props.length || 6;
  var initial = (props.value || "").replace(/[^0-9a-zA-Z]/g, "").slice(0, length).split("");

  function boxes(target: EventTarget | null): HTMLInputElement[] {
    var root = (target as HTMLElement).closest("[data-krate-otp-field]");
    return root ? (Array.prototype.slice.call(root.querySelectorAll(".krate-otp-field-input")) as HTMLInputElement[]) : [];
  }

  function sync(target: EventTarget | null) {
    var inputs = boxes(target);
    var full = inputs.map(function (el) { return el.value; }).join("");
    // Remember each box so a rejected character can put the old digit back.
    inputs.forEach(function (el) { el.setAttribute("data-prev", el.value); });
    var root = (target as HTMLElement).closest("[data-krate-otp-field]");
    var hidden = root ? (root.querySelector("input[data-otp-value]") as HTMLInputElement | null) : null;
    if (hidden) hidden.value = full;
    if (props.onValueChange) props.onValueChange(full);
    if (full.length === inputs.length && props.onComplete) props.onComplete(full);
  }

  // Writes chars into the boxes from `start` on and focuses the next empty one.
  function fill(target: EventTarget | null, start: number, chars: string[]) {
    var inputs = boxes(target);
    for (var i = 0; i < chars.length && start + i < inputs.length; i++) {
      inputs[start + i].value = chars[i];
    }
    var next = Math.min(start + chars.length, inputs.length - 1);
    if (inputs[next]) inputs[next].focus();
  }

  function handleInput(e: Event) {
    var target = e.target as HTMLInputElement;
    var index = Number(target.getAttribute("data-otp-index"));
    var ie = e as InputEvent;
    var chars: string[];
    if (ie.inputType === "insertText" && ie.data && ie.data.length === 1) {
      // Typing over a filled box replaces it.
      chars = /^[0-9a-zA-Z]$/.test(ie.data) ? [ie.data] : [];
    } else {
      // Autofill and other whole-value edits: spread the characters out.
      chars = target.value.replace(/[^0-9a-zA-Z]/g, "").split("");
    }
    if (chars.length === 0) {
      var cleared = ie.inputType !== "insertText" && ie.inputType !== "insertReplacementText";
      target.value = cleared ? "" : (target.getAttribute("data-prev") || "");
    } else {
      fill(target, index, chars);
    }
    sync(target);
  }

  function handleKeyDown(e: Event) {
    var ke = e as KeyboardEvent;
    var target = ke.target as HTMLInputElement;
    var index = Number(target.getAttribute("data-otp-index"));
    var inputs = boxes(target);
    if (ke.key === "Backspace") {
      if (target.value === "" && index > 0) {
        // Empty box: step back and clear the previous digit.
        ke.preventDefault();
        inputs[index - 1].value = "";
        inputs[index - 1].focus();
        sync(target);
      }
      // A filled box lets the browser delete its digit; the input event syncs.
    } else if (ke.key === "ArrowLeft" && index > 0) {
      ke.preventDefault();
      inputs[index - 1].focus();
    } else if (ke.key === "ArrowRight" && index < inputs.length - 1) {
      ke.preventDefault();
      inputs[index + 1].focus();
    } else if (ke.key === "Home") {
      ke.preventDefault();
      inputs[0].focus();
    } else if (ke.key === "End") {
      ke.preventDefault();
      inputs[inputs.length - 1].focus();
    }
  }

  function handleFocus(e: Event) {
    (e.target as HTMLInputElement).select();
  }

  function handlePaste(e: ClipboardEvent) {
    e.preventDefault();
    var data = e.clipboardData || (window as any).clipboardData;
    var text = (data ? data.getData("text") : "").replace(/[^0-9a-zA-Z]/g, "");
    var inputs = boxes(e.target);
    if (inputs.length === 0 || text === "") return;
    // A pasted code replaces the whole field.
    var chars = text.slice(0, inputs.length).split("");
    for (var i = 0; i < inputs.length; i++) {
      inputs[i].value = chars[i] || "";
    }
    var focusIdx = Math.min(chars.length, inputs.length - 1);
    inputs[focusIdx].focus();
    sync(e.target);
  }

  var inputElements = [];
  for (var i = 0; i < length; i++) {
    inputElements.push(
      <input
        class="krate-otp-field-input"
        type="text"
        inputMode="numeric"
        autoComplete={i === 0 ? "one-time-code" : "off"}
        value={initial[i] || ""}
        data-prev={initial[i] || ""}
        autoFocus={props.autoFocus && i === 0 ? true : undefined}
        aria-label={(props.label || "Verification code") + " digit " + (i + 1) + " of " + length}
        data-otp-index={String(i)}
        disabled={props.disabled}
        onInput={handleInput}
        onKeyDown={handleKeyDown}
        onFocus={handleFocus}
      />
    );
  }

  return (
    <div class="krate-otp-field" role="group" aria-label={props.label || "Verification code"} data-krate-otp-field="true" data-length={length} onPaste={handlePaste}>
      {inputElements}
      {props.name ? <input type="hidden" name={props.name} value={initial.join("")} data-otp-value="true" disabled={props.disabled} /> : null}
    </div>
  );
}
