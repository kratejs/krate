export default function Fixture() {
  return (
    <select multiple defaultValue={["a", "c"]}>
      <option value="a">A</option>
      <option value="b">B</option>
      <option value="c">C</option>
    </select>
  );
}
