import './aspect-ratio.css';

export interface AspectRatioProps {
  children?: any;
  ratio?: number;
}

export function AspectRatio(props: AspectRatioProps) {
  var children = props.children || "";
  var ratio = props.ratio || 1;
  var paddingBottom = (1 / ratio) * 100;

  return (
    <div class="krate-aspect-ratio" style={"padding-bottom: " + paddingBottom + "%; position: relative;"}>
      <div class="krate-aspect-ratio-content">
        {children}
      </div>
    </div>
  );
}
