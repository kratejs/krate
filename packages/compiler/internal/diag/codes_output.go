package diag

// KR714x: input that would otherwise make the build output silently wrong.
const (
	// CodeTailwindDisabled: Tailwind input is present but tailwind.enabled is off.
	CodeTailwindDisabled Code = "KR7140"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeTailwindDisabled,
			Title: "Tailwind input found but Tailwind is not enabled",
			Explanation: "Krate generates Tailwind utility CSS only when `tailwind: { enabled: true }` is set in " +
				"`krate.config.ts`. Without it, `@tailwind base|components|utilities` and `@import \"tailwindcss\"` " +
				"are removed from your CSS and no utilities are generated, so the page ships almost no styles. " +
				"The warning appears once per build when a `tailwind.config.{ts,js,mjs}` file exists or one of those " +
				"directives is found. Enable Tailwind, or delete the config file and directives if you do not use it. " +
				"This is a warning.",
		},
	)
}
