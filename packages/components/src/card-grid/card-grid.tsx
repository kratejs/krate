import './card-grid.css';

export interface CardGridProps {
  children?: any;
  /** Extra classes for the root element. */
  class?: string;
  /** Alias of `class`. */
  className?: string;
  id?: string;
}

export function CardGrid(props: CardGridProps) {
  var children = props.children || "";
  return (
    <div class={"krate-card-grid" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} id={props.id}>
      {children}
    </div>
  );
}
