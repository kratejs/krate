package scheme_test

import (
	"time"

	"github.com/kratejs/krate/packages/native/krate"
)

// Task and Add stand for the user's Go code in src/lib/tasks.go.
type Task struct {
	ID      int64
	Title   string
	Done    bool
	Created time.Time
}

func Add(ctx krate.Ctx, title string) (Task, error) {
	if title == "" {
		return Task{}, krate.Errorf("invalid", "title is empty")
	}
	return Task{ID: 1, Title: title, Created: time.Date(2026, 11, 11, 7, 23, 15, 0, time.UTC)}, nil
}
