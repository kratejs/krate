export default function Fixture() {
  return (
    <ul>
      {[{ id: 1, name: "A" }, { id: 2, name: "B" }].map((u) => (
        <li key={u.id}>{u.name}</li>
      ))}
    </ul>
  );
}
