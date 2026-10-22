export default function Fixture() {
  return (
    <select>
      {true && <option value="a">A</option>}
      {false && <option value="b">B</option>}
    </select>
  );
}
