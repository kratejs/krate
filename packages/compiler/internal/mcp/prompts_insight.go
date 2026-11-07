package mcp

// Prompts for the live-app, page-truth and quality tools. Each names the exact
// tools in the order an agent should use them and keeps the dry-run-before-apply
// convention. Evaluated in prompt_eval_test.go.

func (s *Service) promptDebugRunningApp() *Prompt {
	return &Prompt{
		Name:        "debug-running-app",
		Description: "Find and fix what is broken in the running app: build errors, browser errors and server errors from the live dev server.",
		Arguments: []PromptArgument{
			{Name: "route", Description: "Route to focus on", Default: "the whole app"},
		},
		Messages: []PromptMessage{{
			Role:    "user",
			Content: TextContent("Debug the Krate app for {{route}}. Call dev_server_status; if no dev server is running, start one with dev_server (dry-run first, then apply). Call get_errors for build, browser and server errors; for each KR code run explain_diagnostic. Read get_logs around the failure, and use page_check when only a real browser can show the problem. Read the files the errors name, repair them with edit_file or apply_patch (dry-run diff first, apply after review), then call get_errors again to confirm the app is clean. Report anything that remains."),
		}},
	}
}

func (s *Service) promptDiagnosePage() *Prompt {
	return &Prompt{
		Name:        "diagnose-page",
		Description: "Explain why a page renders, hydrates and weighs what it does, and propose the smallest changes.",
		Arguments: []PromptArgument{
			{Name: "route", Description: "Route to diagnose, e.g. /pricing", Required: true},
			{Name: "question", Description: "What to find out", Default: "why it is slow or interactive"},
		},
		Messages: []PromptMessage{{
			Role:    "user",
			Content: TextContent("Diagnose {{route}}: {{question}}. Run explain_page for its mode, tiers, islands and bundle cost, why_hydrated for the file:line reasons it ships JavaScript, why_in_bundle for any heavy module or package it pulls in, get_page_metadata for its head, and audit_page scoped to the route. Propose the smallest changes (move state into a small client component, client:visible, @server for static parts) as a dry-run diff, and apply only after review. Re-run explain_page afterwards and compare the byte totals."),
		}},
	}
}

func (s *Service) promptUpgradeKrate() *Prompt {
	return &Prompt{
		Name:        "upgrade-krate",
		Description: "Upgrade the project from Krate 0.4 to 0.5 with the guided upgrade tool, safe codemods first.",
		Messages: []PromptMessage{{
			Role:    "user",
			Content: TextContent("Upgrade this project to Krate 0.5. Call upgrade (dry-run) and read every step. Review the safe codemods, then call upgrade with apply: true. For each manual step make the edit it describes (scripts, Dockerfiles, CI, Tailwind) with edit_file after reading the file; never change dependency versions without asking. Run build and then typecheck, use explain_diagnostic on every KR code that appears, and finish with get_errors or get_diagnostics. Summarize what changed and what is left."),
		}},
	}
}

func (s *Service) promptMigrateFromNextjs() *Prompt {
	return &Prompt{
		Name:        "migrate-from-nextjs",
		Description: "Plan and verify a Next.js to Krate migration without surprises.",
		Messages: []PromptMessage{{
			Role:    "user",
			Content: TextContent("Plan the Next.js to Krate migration. Call migrate_nextjs and read the route inventory, conflicts, overwrites and manual items. Use compat_check for each npm package the app depends on and why_in_bundle for heavy ones. Present the plan and the manual decisions before anything is written; the plan itself changes no files. Applying is `krate migrate nextjs --apply` (it backs up overwritten files), which the user runs. Afterwards run build, get_diagnostics, explain_diagnostic for each code, and check, and fix what remains with edit_file or apply_patch."),
		}},
	}
}

func (s *Service) promptPerformanceReview() *Prompt {
	return &Prompt{
		Name:        "performance-review",
		Description: "Review bundle size and hydration cost across the site and rank what to fix.",
		Messages: []PromptMessage{{
			Role:    "user",
			Content: TextContent("Review the site's performance. Run build, then bundle_report for the heaviest routes and any budget overruns, list_islands for the components and packages that hydrate most widely, and explain_page and why_hydrated for the top three routes. Use why_in_bundle on the largest dependencies. Report a ranked list of fixes with the bytes each would save and the files to change. Do not modify files; propose diffs only."),
		}},
	}
}
