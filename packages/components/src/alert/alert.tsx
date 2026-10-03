import './alert.css';

export interface AlertProps {
  children?: any;
  variant?: 'default' | 'info' | 'success' | 'warning' | 'destructive';
  title?: string;
}

function AlertIcon(props: { variant: string }) {
  if (props.variant === "success") {
    return <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg>;
  } else if (props.variant === "warning") {
    return <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"/><path d="M12 9v4"/><path d="M12 17h.01"/></svg>;
  } else if (props.variant === "destructive") {
    return <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" x2="12" y1="8" y2="12"/><line x1="12" x2="12.01" y1="16" y2="16"/></svg>;
  }
  return <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg>;
}

export function Alert(props: AlertProps) {
  var variant = props.variant || "default";
  return (
    <div class={"krate-alert krate-alert-" + variant} role={variant === "destructive" ? "alert" : "status"}>
      <span class="krate-alert-icon"><AlertIcon variant={variant} /></span>
      <div class="krate-alert-body">
        {props.title ? <div class="krate-alert-title">{props.title}</div> : null}
        <div class="krate-alert-description">{props.children}</div>
      </div>
    </div>
  );
}
