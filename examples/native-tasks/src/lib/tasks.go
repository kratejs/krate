package lib

import (
	"runtime"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
)

// Task is one item of the list.
type Task struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// Count is a host signal: the UI reads it like a reactive value.
var Count = krate.NewSignal(0)

var (
	mu    sync.Mutex
	tasks = []Task{{ID: 1, Title: "Read the Krate Native docs"}}
	next  = 2
)

func recount() {
	open := 0
	for _, t := range tasks {
		if !t.Done {
			open++
		}
	}
	Count.Set(open)
}

// Edition is evaluated while the app is built (//krate:build); the UI gets the
// baked string.
//
//krate:build
func Edition(name string) string { return "Krate " + name + " edition, built with " + runtime.Version() }

// List returns every task.
func List() []Task {
	mu.Lock()
	defer mu.Unlock()
	return append([]Task(nil), tasks...)
}

// Get returns one task, or a not_found error the UI sees as a KrateError.
func Get(id int) (Task, error) {
	mu.Lock()
	defer mu.Unlock()
	for _, t := range tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return Task{}, krate.Errorf("not_found", "no task %d", id)
}

// Add appends a task.
func Add(title string) (Task, error) {
	if title == "" {
		return Task{}, krate.Errorf("invalid", "a task needs a title")
	}
	mu.Lock()
	defer mu.Unlock()
	t := Task{ID: next, Title: title}
	next++
	tasks = append(tasks, t)
	recount()
	return t, nil
}

// Toggle flips the done flag of a task.
func Toggle(id int) (Task, error) {
	mu.Lock()
	defer mu.Unlock()
	for i := range tasks {
		if tasks[i].ID == id {
			tasks[i].Done = !tasks[i].Done
			recount()
			return tasks[i], nil
		}
	}
	return Task{}, krate.Errorf("not_found", "no task %d", id)
}

// Watch streams the number of tasks once a second until the UI stops
// listening.
func Watch(ctx krate.Ctx) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			mu.Lock()
			n := len(tasks)
			mu.Unlock()
			select {
			case out <- n:
			case <-ctx.Done():
				return
			}
			select {
			case <-tick.C:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

func init() { recount() }
