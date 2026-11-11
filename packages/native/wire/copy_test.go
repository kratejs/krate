package wire

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCopyVectors(t *testing.T) {
	for _, bin := range vectors(t) {
		name := strings.TrimSuffix(bin, ".bin")
		t.Run(name, func(t *testing.T) {
			frame, err := os.ReadFile(bin)
			if err != nil {
				t.Fatal(err)
			}
			d, err := NewDecoder(frame)
			if err != nil {
				t.Fatal(err)
			}
			e := NewEncoder()
			if err := Copy(e, d); err != nil {
				t.Fatal(err)
			}
			if err := d.Finish(); err != nil {
				t.Fatal(err)
			}
			got, err := e.Finish()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, frame) {
				t.Errorf("copy differs\n got %x\nwant %x", got, frame)
			}
		})
	}
}

func TestCopyReindexesBlobs(t *testing.T) {
	src := NewEncoder()
	src.BeginArray()
	src.Bytes([]byte("one"))
	src.BeginMap()
	src.BeginArray()
	src.Bytes([]byte("two"))
	src.Time(time.Date(2026, 11, 11, 7, 23, 15, 123e6, time.UTC))
	src.EndArray()
	src.EndMap()
	src.EndArray()
	frame, err := src.Finish()
	if err != nil {
		t.Fatal(err)
	}

	// Copy two values into one encoder: the second's blob index must shift.
	dst := NewEncoder()
	dst.BeginArray()
	for range 2 {
		d, _ := NewDecoder(frame)
		if err := Copy(dst, d); err != nil {
			t.Fatal(err)
		}
	}
	dst.EndArray()
	out, err := dst.Finish()
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewDecoder(out)
	if err != nil {
		t.Fatal(err)
	}
	if d.Blobs() != 4 {
		t.Fatalf("blobs = %d, want 4", d.Blobs())
	}
	if err := d.BeginArray(); err != nil {
		t.Fatal(err)
	}
	var seen []string
	for d.More() {
		e := NewEncoder()
		if err := Copy(e, d); err != nil {
			t.Fatal(err)
		}
		f, _ := e.Finish()
		if !bytes.Equal(f, frame) {
			t.Errorf("element differs after re-indexing")
		}
		seen = append(seen, "ok")
	}
	if len(seen) != 2 {
		t.Fatalf("elements = %d", len(seen))
	}
}

func TestCopyErrors(t *testing.T) {
	d, err := NewDecoder([]byte("\x00\x00\x00\x03[1,\x00\x00"))
	if err == nil {
		if err = Copy(NewEncoder(), d); err == nil {
			t.Error("expected an error for a truncated array")
		}
	}
}
