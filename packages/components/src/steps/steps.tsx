import './steps.css';

export interface StepsProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

export function Steps(props: StepsProps) {
  var children = props.children || "";
  return (
    <div class={"krate-steps" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}>
      {children}
    </div>
  );
}

export interface StepProps {
  children?: any;
  title?: string;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Steps.Step` instead. */
export function Step(props: StepProps) {
  var children = props.children || "";
  var title = props.title || "";
  return (
    <div class={"krate-step" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}>
      <div class="krate-step-header">
        <div class="krate-step-number"></div>
      </div>
      {title !== "" ? <div class="krate-step-title">{title}</div> : <></>}
      <div class="krate-step-content">
        {children}
      </div>
    </div>
  );
}

export interface Steps {
  Step: typeof Step;
}

Steps.Step = Step;
