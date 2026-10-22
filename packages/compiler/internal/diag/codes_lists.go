package diag

// Lists and keys (KR6xxx).
const (
	// CodeListMissingKey: a dynamic list renders elements without a key.
	CodeListMissingKey Code = "KR6001"
	// CodeListDuplicateKey: two items of a list fold to the same key at build time.
	CodeListDuplicateKey Code = "KR6002"
	// CodeListPartialKey: a map callback keys some returned elements but not others.
	CodeListPartialKey Code = "KR6003"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeListMissingKey,
			Title: "Each child in a list should have a unique key prop",
			Explanation: "A list whose items can change at runtime (its array reads a signal, state or a " +
				"runtime value) returns elements without a key, so rows are matched by index. When items " +
				"are reordered, inserted or removed, row state (input values, focus, component state, " +
				"effects) stays with the position instead of following the item, exactly as in React. " +
				"Add a stable, unique key from the item, for example <li key={item.id}>. Lists over " +
				"build-time constants are not reported.",
		},
		CodeInfo{
			Code:  CodeListDuplicateKey,
			Title: "Encountered two children with the same key",
			Explanation: "Two items of a list resolve to the same key at build time. Keys must be unique " +
				"among siblings so each row keeps its identity. The runtime also logs a console error " +
				"when it sees duplicates; it keeps the first occurrence on its existing row and renders " +
				"each duplicate as a separate row. Use an id that is unique per item.",
		},
		CodeInfo{
			Code:  CodeListPartialKey,
			Title: "List callback keys only some of its elements",
			Explanation: "A map callback returns a keyed element on some paths and an unkeyed one on " +
				"others (for example a ternary with a key on one branch). Unkeyed rows fall back to " +
				"their index, which mixes identities. Put the same key on every returned element.",
		},
	)
}
