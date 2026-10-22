export default function Fixture() {
  return (
    <ul>
      {true ? <li>a</li> : <li>b</li>}
      {false && <li>c</li>}
      <li>d</li>
    </ul>
  );
}
