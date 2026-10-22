export default function Fixture() {
  return (
    <div>
      <input type="checkbox" checked readOnly />
      <input type="checkbox" checked={false} readOnly />
    </div>
  );
}
