import { useParams } from "@krate/runtime";

export default function Docs() {
  const params = useParams();
  return <p id="docs-rest">{Array.isArray(params.rest) ? params.rest.join("+") : "none"}</p>;
}
