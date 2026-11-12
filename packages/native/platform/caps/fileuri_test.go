package caps

import (
	"reflect"
	"testing"
)

func TestParseURIList(t *testing.T) {
	got := ParseURIList("# a comment\r\nfile:///tmp/a.txt\r\n\r\nfile:///tmp/b%20c.txt\nhttps://example.com/x\n")
	want := []string{"file:///tmp/a.txt", "file:///tmp/b%20c.txt", "https://example.com/x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestPathsFromURIs(t *testing.T) {
	got := PathsFromURIs([]string{
		"file:///tmp/a.txt", "file://localhost/tmp/b%20c.txt", "FILE:///tmp/ünï.txt", "file:///tmp/a.txt",
		"https://example.com/x", "file://otherhost/tmp/x", "file:///", "", "not a uri %%", "file:///tmp/dir/",
	})
	want := []string{"/tmp/a.txt", "/tmp/b c.txt", "/tmp/ünï.txt", "/", "/tmp/dir"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}
