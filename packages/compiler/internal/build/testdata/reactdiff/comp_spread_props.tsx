function Box(props: any) {
  return <div {...props}>x</div>;
}

export default function Fixture() {
  const p = { id: "i", className: "c", "data-x": "1" };
  return <Box {...p} title="t" />;
}
