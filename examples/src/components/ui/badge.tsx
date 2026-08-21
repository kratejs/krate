export default function Badge(props: { label: string }) {
  return (
    <span class="badge">
      {props.label}
    </span>
  );
}