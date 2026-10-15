import { KrateBadge } from "../components/ui/KrateBadge.server.go";

// Demonstrates importing a Go-authored server component (see
// src/components/ui/KrateBadge.server.go) and rendering it at build time.
export default function GoComponentPage() {
	return (
		<main>
			<h1>Go Component</h1>
			<KrateBadge label="go-native" items={["typed props", "escaped by default", "single binary"]} />
		</main>
	);
}
