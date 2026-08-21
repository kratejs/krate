import './steps.css';

export interface StepsProps {
  children?: any;
}

export default function Steps(props: StepsProps) {
  var children = props.children || "";
  return (
    <div class="krate-steps">
      {children}
    </div>
  );
}

export interface StepProps {
  children?: any;
  title?: string;
}

export function Step(props: StepProps) {
  var children = props.children || "";
  var title = props.title || "";
  return (
    <div class="krate-step">
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
