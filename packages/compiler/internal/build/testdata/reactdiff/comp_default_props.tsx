function Badge({ label = "none", tone = "info" }: { label?: string; tone?: string }) {
  return <span className={tone}>{label}</span>;
}

export default function Fixture() {
  return (
    <div>
      <Badge />
      <Badge label="x" />
      <Badge tone="warn" label="y" />
    </div>
  );
}
