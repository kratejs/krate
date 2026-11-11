package lib

// CardProps are the props of the Card component.
type CardProps struct {
	Title string `json:"title"`
}

// Card is a Go component, not a host function.
func Card(p CardProps) string { return "<div>" + p.Title + "</div>" }
