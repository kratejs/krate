import './card-grid.css';

export interface CardGridProps {
  children?: any;
}

export default function CardGrid(props: CardGridProps) {
  var children = props.children || "";
  return (
    <div class="krate-card-grid">
      {children}
    </div>
  );
}
