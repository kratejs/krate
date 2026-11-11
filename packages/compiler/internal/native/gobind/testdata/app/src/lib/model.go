package lib

import "time"

// Status is a named string.
type Status string

// Labels uses a string-kinded named key.
type Labels map[Status]int

// Meta is a pointer target with a map and a narrow float.
type Meta struct {
	Labels map[string]string `json:"labels"`
	Weight float32
}

// Base is flattened into Task like encoding/json does.
type Base struct {
	ID      int64     `json:"id"`
	Created time.Time `json:"created"`
}

// Task covers tags, omitempty, "-", embedding, nesting, pointers, time,
// bytes, durations and a recursive field.
type Task struct {
	Base
	Title    string `json:"title"`
	Note     string `json:"note,omitempty"`
	Secret   string `json:"-"`
	hidden   int
	Status   Status             `json:"status"`
	Tags     []string           `json:"tags"`
	Grid     [][]int            `json:"grid"`
	Meta     *Meta              `json:"meta"`
	Attrs    map[string][]int32 `json:"attrs,omitempty"`
	Blob     []byte             `json:"blob"`
	Due      *time.Time         `json:"due,omitempty"`
	Timeout  time.Duration      `json:"timeout"`
	Small    uint8
	Pair     [2]string `json:"pair"`
	Children []*Task   `json:"children,omitempty"`
}

// Tree is recursive through a slice.
type Tree struct {
	Name string
	Kids []Tree
}

// Echoed carries bytes and a time back.
type Echoed struct {
	Data []byte    `json:"data"`
	At   time.Time `json:"at"`
}
