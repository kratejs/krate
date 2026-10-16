import './aside.css';

export interface AsideProps {
  children?: any;
  type?: string;
  title?: string;
  icon?: string;
}

function AsideIcon(props: { type: string }) {
  if (props.type === "tip") {
    return <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 14c.2-1 .7-1.7 1.5-2.5 1-.9 1.5-2.2 1.5-3.5A6 6 0 0 0 6 8c0 1 .2 2.2 1.5 3.5.7.7 1.3 1.5 1.5 2.5"/><path d="M9 18h6"/><path d="M10 22h4"/></svg>;
  } else if (props.type === "warning") {
    return <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"/><path d="M12 9v4"/><path d="M12 17h.01"/></svg>;
  } else if (props.type === "danger") {
    return <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="9" cy="12" r="1"/><circle cx="15" cy="12" r="1"/><path d="M8 20v2h8v-2"/><path d="m12.5 17-.5-1-.5 1h1z"/><path d="M16 20a2 2 0 0 0 1.56-3.25 8 8 0 1 0-11.12 0A2 2 0 0 0 8 20"/></svg>;
  } else if (props.type === "caution") {
    return <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" x2="12" y1="8" y2="12"/><line x1="12" x2="12.01" y1="16" y2="16"/></svg>;
  } else {
    return <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg>;
  }
}

export function Aside(props: AsideProps) {
  var children = props.children || "";
  var type = props.type || "note";
  var title = props.title || "";
  if (title === "") {
    if (type === "tip") {
      title = "Tip";
    } else if (type === "warning") {
      title = "Warning";
    } else if (type === "danger") {
      title = "Danger";
    } else if (type === "caution") {
      title = "Caution";
    } else {
      title = "Note";
    }
  }
  return (
    <div class={"krate-aside krate-aside-" + type}>
      <div class="krate-aside-title"><AsideIcon type={type} /> {title}</div>
      <div class="krate-aside-content">{children}</div>
    </div>
  );
}
