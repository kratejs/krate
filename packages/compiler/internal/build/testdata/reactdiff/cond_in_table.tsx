export default function Fixture() {
  return (
    <table>
      <tbody>
        {true && <tr><td>a</td></tr>}
        {false && <tr><td>b</td></tr>}
        <tr><td>c</td></tr>
      </tbody>
    </table>
  );
}
