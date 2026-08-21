export default function RuntimeCard(props: { title: string }) {
  return (
    <div class="runtime-card">
      <h4>{props.title}</h4>
      <p>Rendered at serve time: {Date.now()}</p>
    </div>
  );
}
