export default function Fixture() {
  return (
    <div dangerouslySetInnerHTML={{ __html: "<b>bold</b> & <i>it</i>" }} />
  );
}
