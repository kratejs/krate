package mcp

// registerInsightTools installs the tools that read the live dev server, the
// compiled site and the project's configuration. See devserver.go,
// tools_page.go, tools_project.go, tools_quality.go and tools_browser.go.
func (s *Service) registerInsightTools(srv *Server) {
	s.registerDevTools(srv)
	s.registerPageTools(srv)
	s.registerProjectTools(srv)
	s.registerQualityTools(srv)
	s.registerBrowserTools(srv)
}

// registerInsightPrompts adds the workflows for the tools above, after the
// original prompts so the list keeps its order.
func (s *Service) registerInsightPrompts(srv *Server) {
	srv.RegisterPrompt(s.promptDebugRunningApp())
	srv.RegisterPrompt(s.promptDiagnosePage())
	srv.RegisterPrompt(s.promptUpgradeKrate())
	srv.RegisterPrompt(s.promptMigrateFromNextjs())
	srv.RegisterPrompt(s.promptPerformanceReview())
	srv.RegisterCompletion("ref/prompt", "diagnose-page", "route", s.completeRoutes)
	srv.RegisterCompletion("ref/prompt", "debug-running-app", "route", s.completeRoutes)
	for _, tool := range []string{"explain_page", "why_hydrated", "get_page_metadata", "get_rendered_html", "render_preview", "why_in_bundle", "page_check", "browser_eval", "audit_page"} {
		srv.RegisterCompletion("ref/tool", tool, "route", s.completeRoutes)
	}
}
