export default function RuntimeWidget(props: { label: string }) {
  return (
    <div class="runtime-widget">
      <h3>{props.label}</h3>
      <p class="runtime-note">Rendered at serve time: {Date.now()}</p>
    </div>
  );
}
