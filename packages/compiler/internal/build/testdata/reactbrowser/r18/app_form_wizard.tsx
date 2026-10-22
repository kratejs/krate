import { useState } from "react";
import { log } from "../lib/log";

type Data = { name: string; email: string; plan: string; terms: boolean };

function validate(step: number, d: Data): Record<string, string> {
  const errors: Record<string, string> = {};
  if (step === 0) {
    if (d.name.trim().length < 2) errors.name = "Name is too short";
    if (!/^\S+@\S+\.\S+$/.test(d.email)) errors.email = "Email is invalid";
  }
  if (step === 1 && !d.plan) errors.plan = "Pick a plan";
  if (step === 2 && !d.terms) errors.terms = "Accept the terms";
  return errors;
}

// Multi-step form: per-step validation, back/next keeping data, and a final
// summary.
export default function AppFormWizard() {
  const [step, setStep] = useState(0);
  const [data, setData] = useState<Data>({ name: "", email: "", plan: "", terms: false });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [done, setDone] = useState(false);

  const set = (patch: Partial<Data>) => setData({ ...data, ...patch });
  const next = () => {
    const e = validate(step, data);
    setErrors(e);
    log("validate step", step, Object.keys(e).join(",") || "ok");
    if (Object.keys(e).length === 0) {
      if (step === 2) setDone(true);
      else setStep(step + 1);
    }
  };

  if (done) {
    return (
      <p id="summary">
        {data.name} &lt;{data.email}&gt; on {data.plan}
      </p>
    );
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        next();
      }}
    >
      <p id="progress">step {step + 1} of 3</p>
      {step === 0 && (
        <fieldset>
          <input id="name" placeholder="name" value={data.name} onChange={(e) => set({ name: e.target.value })} />
          {errors.name && <span className="error">{errors.name}</span>}
          <input id="email" placeholder="email" value={data.email} onChange={(e) => set({ email: e.target.value })} />
          {errors.email && <span className="error">{errors.email}</span>}
        </fieldset>
      )}
      {step === 1 && (
        <fieldset>
          <select id="plan" value={data.plan} onChange={(e) => set({ plan: e.target.value })}>
            <option value="">choose</option>
            <option value="hobby">hobby</option>
            <option value="pro">pro</option>
          </select>
          {errors.plan && <span className="error">{errors.plan}</span>}
        </fieldset>
      )}
      {step === 2 && (
        <fieldset>
          <label>
            <input id="terms" type="checkbox" checked={data.terms} onChange={(e) => set({ terms: e.target.checked })} />
            I accept
          </label>
          {errors.terms && <span className="error">{errors.terms}</span>}
        </fieldset>
      )}
      {step > 0 && (
        <button id="back" type="button" onClick={() => setStep(step - 1)}>
          back
        </button>
      )}
      <button id="next" type="submit">
        {step === 2 ? "finish" : "next"}
      </button>
    </form>
  );
}
