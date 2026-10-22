const items = [
  { id: 1, label: "One", done: true },
  { id: 2, label: "Two", done: false },
];

export default function Fixture() {
  return (
    <ul>
      {items.map((it) => (
        <li key={it.id} className={it.done ? "done" : undefined}>
          {it.label}
        </li>
      ))}
    </ul>
  );
}
