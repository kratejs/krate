import { createSignal, createMemo } from "@krate/runtime";
import { OTPField } from "@krate/components/otp-field";
import { Slider } from "@krate/components/slider";
import { Switch } from "@krate/components/switch";
import "./demo.css";

const text = (e: Event) => (e.target as HTMLInputElement).value;

// Every message below is a memo over the fields, so it recomputes only when
// the input it reads changes. The button enables itself from the same memos.
export default function Demo() {
  const [email, setEmail] = createSignal("");
  const [password, setPassword] = createSignal("");
  const [seats, setSeats] = createSignal(5);
  const [agree, setAgree] = createSignal(false);
  const [code, setCode] = createSignal("");
  const [sent, setSent] = createSignal(false);

  const emailError = createMemo(() => {
    if (email() === "") return "Enter your email.";
    return /^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email()) ? "" : "That does not look like an email.";
  });
  const strength = createMemo(() => {
    const p = password();
    let n = 0;
    if (p.length >= 8) n++;
    if (/[a-z]/.test(p) && /[A-Z]/.test(p)) n++;
    if (/\d/.test(p)) n++;
    if (/[^A-Za-z0-9]/.test(p)) n++;
    return n;
  });
  const strengthLabel = createMemo(() => ["Too weak", "Weak", "Fair", "Good", "Strong"][strength()]);
  const codeOk = createMemo(() => code() === "424242");
  const valid = createMemo(() => emailError() === "" && strength() >= 3 && agree() && codeOk());
  const price = createMemo(() => "$" + seats() * 12 + " / month");

  const submit = (e: Event) => {
    e.preventDefault();
    if (valid()) setSent(true);
  };

  return (
    <form class="fm" onSubmit={submit} noValidate>
      <div class="fm-field">
        <label class="dk-label" for="fm-email">Email</label>
        <input id="fm-email" class="dk-input" type="email" autocomplete="off" aria-describedby="fm-email-msg" aria-invalid={emailError() !== "" && email() !== ""} value={email()} onInput={(e) => setEmail(text(e))} />
        <p id="fm-email-msg" class="fm-msg" data-bad={emailError() !== ""}>{emailError() || "Looks good."}</p>
      </div>

      <div class="fm-field">
        <label class="dk-label" for="fm-pw">Password</label>
        <input id="fm-pw" class="dk-input" type="text" autocomplete="off" aria-describedby="fm-pw-msg" value={password()} onInput={(e) => setPassword(text(e))} />
        <div class="fm-meter" data-level={strength()} aria-hidden="true"><i></i><i></i><i></i><i></i></div>
        <p id="fm-pw-msg" class="fm-msg" data-bad={strength() < 3}>{strengthLabel()}. Use 8+ characters, mixed case, a digit and a symbol.</p>
      </div>

      <div class="fm-field">
        <Slider label="Seats" min={1} max={50} defaultValue={5} onInput={(e: Event) => setSeats(Number(text(e)))} />
        <p class="fm-msg"><strong>{seats()}</strong> seats: {price()}</p>
      </div>

      <div class="fm-field fm-line">
        <Switch label="Product updates by email" />
      </div>

      <div class="fm-field">
        <p class="dk-label" id="fm-otp-label">Verification code (try 424242)</p>
        <OTPField length={6} label="Verification code" onValueChange={(v: string) => setCode(v)} />
        <p class="fm-msg" data-bad={!codeOk()}>{codeOk() ? "Code verified." : "Enter all six digits."}</p>
      </div>

      <label class="fm-check">
        <input type="checkbox" checked={agree()} onChange={(e) => setAgree((e.target as HTMLInputElement).checked)} />
        I agree to the terms
      </label>

      <button type="submit" class="dk-btn dk-btn-primary" disabled={!valid()}>Create account</button>
      <p class="fm-done" role="status">{sent() ? "Account created for " + email() + "." : ""}</p>
    </form>
  );
}
