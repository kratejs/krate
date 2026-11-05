import { Gate } from "./_gate";
export const config = { ssr: true };

// The page renders a server component that gates it on every request.
export default function Guarded() {
  return (
    <div>
      <Gate />
      <p id="page">guarded page</p>
    </div>
  );
}
