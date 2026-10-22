export default function Fixture() {
  return (
    <div>
      <p contentEditable>a</p>
      <p contentEditable={false}>b</p>
      <p contentEditable="plaintext-only">c</p>
    </div>
  );
}
