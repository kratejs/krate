import './card.css';

export interface CardProps {
  title?: string;
  children?: any;
}

export function Card(props: CardProps) {
  var title = props.title || "";
  var children = props.children || "";
  return (
    <div class="krate-card">
      {title !== "" ? <div class="krate-card-title">{title}</div> : <></>}
      <div class="krate-card-body">{children}</div>
    </div>
  );
}
