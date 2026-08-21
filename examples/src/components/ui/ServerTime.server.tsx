// @server
export default function ServerTime(props: { label?: string }) {
  const label = props.label || "Build Time";
  return (
    <div class="server-time">
      <span class="server-time-label">{label}</span>
      <span class="server-time-value">Compiled at build time: {Date.now()}</span>
    </div>
  );
}
