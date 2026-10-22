function Nothing() {
  return null;
}

export default function Fixture() {
  return (
    <div>
      a<Nothing />b
    </div>
  );
}
