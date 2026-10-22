function Card(props: { title: string; children?: any }) {
  return (
    <section className="card">
      <h2>{props.title}</h2>
      {props.children}
    </section>
  );
}

export default function Fixture() {
  return (
    <Card title="T">
      <p>body</p>
    </Card>
  );
}
