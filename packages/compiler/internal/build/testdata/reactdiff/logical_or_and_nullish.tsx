export default function Fixture() {
  return (
    <p>{"" || "fallback"} {null ?? "nn"} {0 ?? "zero"} {0 || "or"}</p>
  );
}
