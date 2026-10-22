export default function Fixture() {
  const name = "World";
  const n = 3;
  const items = ["x", "y"];
  return (
    <div>
      <h1>Hello {name}</h1>
      <p>{n * 2} items: {items.length}</p>
    </div>
  );
}
