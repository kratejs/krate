import './link-card.css';

export interface LinkCardProps {
  title?: string;
  href?: string;
  icon?: string;
  children?: any;
}

export default function LinkCard(props: LinkCardProps) {
  var title = props.title || "";
  var href = props.href || "#";
  var icon = props.icon || "";
  return (
    <a class="krate-link-card" href={href}>
      <div class="krate-link-card-content">
        <div class="krate-link-card-title">{title}</div>
        <div class="krate-link-card-description">{props.children}</div>
      </div>
      {icon !== "" ? <Icon class="krate-link-card-icon" name={icon} /> : null}
    </a>
  );
}
