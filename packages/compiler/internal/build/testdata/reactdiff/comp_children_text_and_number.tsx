function W({ children }: { children?: any }) {
  return <div>{children}</div>;
}

export default function Fixture() {
  return (
    <W>
      text {1} <b>b</b>
    </W>
  );
}
