export default function Fixture() {
  return (
    <div>
      <p draggable>a</p>
      <p draggable={false}>b</p>
      <p draggable="true">c</p>
    </div>
  );
}
