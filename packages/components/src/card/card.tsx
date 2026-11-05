import './card.css';

export interface CardProps {
  title?: string;
  /** Heading element for the title (default h3). Pick the level that fits the page outline. */
  titleAs?: 'h2' | 'h3' | 'h4' | 'h5' | 'h6';
  children?: any;
  /** Extra classes for the root element. */
  class?: string;
  /** Alias of `class`. */
  className?: string;
  id?: string;
}

export function Card(props: CardProps) {
  var title = props.title || "";
  var children = props.children || "";
  return (
    <div class={"krate-card" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} id={props.id}>
      {title !== "" ? (props.titleAs === "h2" ? <h2 class="krate-card-title">{title}</h2> : props.titleAs === "h4" ? <h4 class="krate-card-title">{title}</h4> : props.titleAs === "h5" ? <h5 class="krate-card-title">{title}</h5> : props.titleAs === "h6" ? <h6 class="krate-card-title">{title}</h6> : <h3 class="krate-card-title">{title}</h3>) : <></>}
      <div class="krate-card-body">{children}</div>
    </div>
  );
}
